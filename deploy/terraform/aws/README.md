# AWS Terraform Runtime

This root module provisions the AWS foundation and EKS runtime for the SafeConfig showcase
environment.

It creates:

- one VPC
- public and private subnets across two or three availability zones
- internet gateway and public routes
- optional single NAT Gateway for private subnet egress
- baseline EKS cluster and node security groups
- IAM roles and managed policy attachments for EKS
- EKS cluster
- default EKS managed node group
- ECR repositories for SafeConfig images

NAT Gateway is disabled by default to avoid accidental hourly cost. Enable it only when the EKS node
runtime needs private subnet egress:

```bash
terraform apply -var enable_nat_gateway=true
```

## Prerequisites

- AWS account
- AWS CLI credentials or SSO session
- Terraform 1.6+
- `kubectl`

Check identity before planning:

```bash
aws sts get-caller-identity
```

## Usage

```bash
cd deploy/terraform/aws
terraform init
terraform fmt
terraform validate
terraform plan
```

Apply only when you are ready to create billable AWS resources:

```bash
terraform apply
```

## Configure kubectl

After `terraform apply` completes:

```bash
aws eks update-kubeconfig \
  --region "$(terraform output -raw aws_region)" \
  --name "$(terraform output -raw eks_cluster_name)"
```

Equivalent Make target from the repository root:

```bash
make eks-kubeconfig
```

Validate worker nodes:

```bash
kubectl get nodes
```

Equivalent Make target:

```bash
make eks-nodes
```

Destroy the showcase foundation after testing:

```bash
terraform destroy
```

## Cost Notes

- VPC, route tables, subnets, security groups and IAM roles have no direct hourly charge.
- EKS, EC2 worker nodes, EBS volumes, NAT Gateway, load balancers, and managed PostgreSQL can create
  ongoing charges.
- ECR image storage can create ongoing charges while images remain.
- This module defaults `enable_nat_gateway` to `false`.
- This module defaults the node group to public subnets so showcase nodes can reach image registries
  without NAT Gateway. Use `eks_node_subnet_tier = "private"` with NAT enabled for a more hardened
  network shape.

## Kubernetes Deploy

AWS/EKS overlays live in `deploy/kubernetes/aws`.

Render before applying:

```bash
make aws-platform-render
make aws-demo-render
```

Replace the ECR image placeholders before applying to the cluster.
