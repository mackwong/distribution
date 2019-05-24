# Distribution

Docker registry 在使用 cache-through 模式是不允许 push 的， 而且在拉取镜像时，也是优先从 remote registry里获取镜像。

此修改版支持在 cache-through 模式下 push，同时优先从local register 获取镜像。

原 [Readme](https://raw.githubusercontent.com/docker/distribution/master/README.md)
