output "name_prefix" {
  description = "Common resource name prefix."
  value       = local.name_prefix
}

output "aws_region" {
  description = "AWS region used by the foundation stack."
  value       = var.aws_region
}

output "vpc_id" {
  description = "SafeConfig VPC ID."
  value       = aws_vpc.this.id
}

output "public_subnet_ids" {
  description = "Public subnet IDs for load balancers."
  value       = values(aws_subnet.public)[*].id
}

output "private_subnet_ids" {
  description = "Private subnet IDs for EKS nodes."
  value       = values(aws_subnet.private)[*].id
}

output "eks_cluster_role_arn" {
  description = "IAM role ARN for the future EKS cluster."
  value       = aws_iam_role.eks_cluster.arn
}

output "eks_node_role_arn" {
  description = "IAM role ARN for future EKS managed node groups."
  value       = aws_iam_role.eks_nodes.arn
}

output "eks_cluster_security_group_id" {
  description = "Additional security group ID for the future EKS cluster."
  value       = aws_security_group.eks_cluster.id
}

output "eks_node_security_group_id" {
  description = "Security group ID for future EKS nodes."
  value       = aws_security_group.eks_nodes.id
}

output "nat_gateway_enabled" {
  description = "Whether this foundation stack created a NAT Gateway."
  value       = var.enable_nat_gateway
}

output "eks_cluster_name" {
  description = "EKS cluster name."
  value       = aws_eks_cluster.this.name
}

output "eks_cluster_endpoint" {
  description = "EKS cluster API endpoint."
  value       = aws_eks_cluster.this.endpoint
}

output "eks_cluster_certificate_authority_data" {
  description = "Base64 encoded EKS cluster certificate authority data."
  value       = aws_eks_cluster.this.certificate_authority[0].data
  sensitive   = true
}

output "eks_node_group_name" {
  description = "Default EKS managed node group name."
  value       = aws_eks_node_group.default.node_group_name
}

output "eks_node_subnet_tier" {
  description = "Subnet tier used by the managed node group."
  value       = var.eks_node_subnet_tier
}

output "kubeconfig_command" {
  description = "Command to configure local kubectl for the EKS cluster."
  value       = "aws eks update-kubeconfig --region ${var.aws_region} --name ${aws_eks_cluster.this.name}"
}

output "ecr_repository_urls" {
  description = "ECR repository URLs for SafeConfig images."
  value = {
    for name, repository in aws_ecr_repository.safeconfig : name => repository.repository_url
  }
}
