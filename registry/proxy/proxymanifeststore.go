package proxy

import (
	"context"
	"fmt"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	"time"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/proxy/scheduler"
	"github.com/opencontainers/go-digest"
)

// todo(richardscothern): from cache control header or config
const repositoryTTL = 24 * 7 * time.Hour

type proxyManifestStore struct {
	ctx             context.Context
	localManifests  distribution.ManifestService
	remoteManifests distribution.ManifestService
	repositoryName  reference.Named
	scheduler       *scheduler.TTLExpirationScheduler
	authChallenger  authChallenger
}

var _ distribution.ManifestService = &proxyManifestStore{}

func (pms proxyManifestStore) Exists(ctx context.Context, dgst digest.Digest) (bool, error) {
	exists, err := pms.localManifests.Exists(ctx, dgst)
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}
	if err := pms.authChallenger.tryEstablishChallenges(ctx); err != nil {
		return false, err
	}
	return pms.remoteManifests.Exists(ctx, dgst)
}

func (pms proxyManifestStore) Get(ctx context.Context, dgst digest.Digest, options ...distribution.ManifestServiceOption) (distribution.Manifest, error) {
	// At this point `dgst` was either specified explicitly, or returned by the
	// tagstore with the most recent association.
	var fromRemote bool
	manifest, err := pms.localManifests.Get(ctx, dgst, options...)
	if err != nil {
		if err := pms.authChallenger.tryEstablishChallenges(ctx); err != nil {
			return nil, err
		}

		manifest, err = pms.remoteManifests.Get(ctx, dgst, options...)
		if err != nil {
			return nil, err
		}
		fromRemote = true
	}

	_, payload, err := manifest.Payload()
	if err != nil {
		return nil, err
	}

	proxyMetrics.ManifestPush(uint64(len(payload)))
	if fromRemote {
		proxyMetrics.ManifestPull(uint64(len(payload)))

		_, err = pms.localManifests.Put(ctx, manifest)
		if err != nil {
			return nil, err
		}

		// Schedule the manifest blob for removal
		repoBlob, err := reference.WithDigest(pms.repositoryName, dgst)
		if err != nil {
			dcontext.GetLogger(ctx).Errorf("Error creating reference: %s", err)
			return nil, err
		}

		pms.scheduler.AddManifest(repoBlob, repositoryTTL)
		// Ensure the manifest blob is cleaned up
		//pms.scheduler.AddBlob(blobRef, repositoryTTL)

	}

	return manifest, err
}

func (pms proxyManifestStore) Put(ctx context.Context, manifest distribution.Manifest, options ...distribution.ManifestServiceOption) (digest.Digest, error) {
	dcontext.GetLogger(pms.ctx).Debug("(*manifestStore).Put")

	switch manifest.(type) {
	case *schema1.SignedManifest:
		return pms.localManifests.Put(ctx, manifest, options...)
	case *schema2.DeserializedManifest:
		return pms.localManifests.Put(ctx, manifest, options...)
	case *ocischema.DeserializedManifest:
		return pms.localManifests.Put(ctx, manifest, options...)
	case *manifestlist.DeserializedManifestList:
		return pms.localManifests.Put(ctx, manifest, options...)
	}

	return "", fmt.Errorf("unrecognized manifest type %T", manifest)
}

func (pms proxyManifestStore) Delete(ctx context.Context, dgst digest.Digest) error {
	return distribution.ErrUnsupported
}
