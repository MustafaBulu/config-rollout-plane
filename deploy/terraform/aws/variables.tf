variable "aws_region" {
  description = "AWS region for the SafeConfig showcase environment."
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Short name used to prefix AWS resources."
  type        = string
  default     = "safeconfig"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,30}[a-z0-9]$", var.project_name))
    error_message = "project_name must start with a lowercase letter and contain only lowercase letters, digits, and hyphens."
  }
}

variable "environment" {
  description = "Environment label for the AWS showcase resources."
  type        = string
  default     = "showcase"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,30}[a-z0-9]$", var.environment))
    error_message = "environment must start with a lowercase letter and contain only lowercase letters, digits, and hyphens."
  }
}

variable "vpc_cidr" {
  description = "CIDR block for the SafeConfig AWS VPC."
  type        = string
  default     = "10.42.0.0/16"
}

variable "availability_zone_count" {
  description = "Number of availability zones to use."
  type        = number
  default     = 2

  validation {
    condition     = var.availability_zone_count >= 2 && var.availability_zone_count <= 3
    error_message = "availability_zone_count must be 2 or 3."
  }
}

variable "public_subnet_cidrs" {
  description = "CIDR blocks for public subnets. Provide at least availability_zone_count entries."
  type        = list(string)
  default     = ["10.42.0.0/20", "10.42.16.0/20", "10.42.32.0/20"]
}

variable "private_subnet_cidrs" {
  description = "CIDR blocks for private subnets. Provide at least availability_zone_count entries."
  type        = list(string)
  default     = ["10.42.128.0/20", "10.42.144.0/20", "10.42.160.0/20"]
}

variable "enable_nat_gateway" {
  description = "Create one NAT Gateway for private subnet egress. Disabled by default to avoid accidental cost."
  type        = bool
  default     = false
}

variable "eks_cluster_version" {
  description = "Kubernetes version for the EKS cluster."
  type        = string
  default     = "1.31"
}

variable "eks_endpoint_public_access" {
  description = "Enable public EKS API endpoint access for local kubectl validation."
  type        = bool
  default     = true
}

variable "eks_endpoint_private_access" {
  description = "Enable private EKS API endpoint access inside the VPC."
  type        = bool
  default     = true
}

variable "eks_node_subnet_tier" {
  description = "Subnet tier for the managed node group. Use public to avoid NAT Gateway during showcase, private for hardened runs."
  type        = string
  default     = "public"

  validation {
    condition     = contains(["public", "private"], var.eks_node_subnet_tier)
    error_message = "eks_node_subnet_tier must be public or private."
  }
}

variable "eks_node_instance_types" {
  description = "EC2 instance types for the EKS managed node group."
  type        = list(string)
  default     = ["t3.small"]
}

variable "eks_node_capacity_type" {
  description = "Capacity type for the EKS managed node group."
  type        = string
  default     = "ON_DEMAND"

  validation {
    condition     = contains(["ON_DEMAND", "SPOT"], var.eks_node_capacity_type)
    error_message = "eks_node_capacity_type must be ON_DEMAND or SPOT."
  }
}

variable "eks_node_desired_size" {
  description = "Desired number of EKS worker nodes."
  type        = number
  default     = 2
}

variable "eks_node_min_size" {
  description = "Minimum number of EKS worker nodes."
  type        = number
  default     = 1
}

variable "eks_node_max_size" {
  description = "Maximum number of EKS worker nodes."
  type        = number
  default     = 3
}

variable "eks_node_disk_size_gb" {
  description = "Root disk size in GiB for EKS worker nodes."
  type        = number
  default     = 30
}
