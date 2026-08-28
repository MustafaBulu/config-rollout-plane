# AWS Kubernetes Overlays

These overlays deploy SafeConfig to the EKS cluster created by `deploy/terraform/aws`.

They reuse the local Kubernetes base and apply AWS/EKS-specific image, replica, resource, and storage
settings.

## Image Placeholders

Terraform creates ECR repositories when `create_ecr_repositories = true`.

After `terraform apply`, inspect them:

```bash
terraform -chdir=deploy/terraform/aws output ecr_repository_urls
```

Login to ECR:

```bash
aws ecr get-login-password --region "$(terraform -chdir=deploy/terraform/aws output -raw aws_region)" | \
  docker login --username AWS --password-stdin "<account-id>.dkr.ecr.<region>.amazonaws.com"
```

Build and push images:

```bash
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=control-plane -t "<control-plane-repo>:latest" .
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=data-plane -t "<data-plane-repo>:latest" .
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=agent -t "<agent-repo>:latest" .
docker build -t "<payment-demo-service-repo>:latest" examples/demo-service

docker push "<control-plane-repo>:latest"
docker push "<data-plane-repo>:latest"
docker push "<agent-repo>:latest"
docker push "<payment-demo-service-repo>:latest"
```

The overlays intentionally keep placeholder ECR image names:

```text
ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com/safeconfig/control-plane:latest
ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com/safeconfig/data-plane:latest
ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com/safeconfig/agent:latest
ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com/safeconfig/payment-demo-service:latest
```

Before applying, replace `ACCOUNT_ID` and `REGION`, or update the overlay images to the repository
URLs returned by Terraform. Render before applying:

```bash
kubectl kustomize deploy/kubernetes/aws/platform
kubectl kustomize deploy/kubernetes/aws/demo
```

## Deploy Platform

From the repository root:

```bash
make eks-kubeconfig
make eks-nodes
make aws-platform-render
make aws-apply-platform
make aws-wait-platform
```

## Deploy Demo Workload

```bash
make aws-demo-render
make aws-apply-demo
make aws-wait-demo
```

## Smoke Check

```bash
make aws-smoke
kubectl -n safe-config-system port-forward svc/control-plane 8080:8080
```

## Cleanup

Remove Kubernetes workloads before destroying AWS infrastructure:

```bash
make aws-delete-k8s
make terraform-destroy
```
