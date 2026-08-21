# Docker Images

Build the Go control-plane, data-plane, and agent images from the repository root:

```bash
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=control-plane -t safeconfig/control-plane:local .
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=data-plane -t safeconfig/data-plane:local .
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=agent -t safeconfig/agent:local .
```

Build the demo service image:

```bash
docker build -t safeconfig/payment-demo-service:local examples/demo-service
```
