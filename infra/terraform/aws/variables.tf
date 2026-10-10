variable "region" {
  type    = string
  default = "us-west-2"
}

variable "account_id" {
  type        = string
  default     = "000000000000"
  description = "New dedicated AWS account. Do not use 308639168050."
}

variable "desired_count" {
  type        = number
  default     = 0
  description = "Stays 0 so a mistaken apply does not start billable Fargate tasks."
}

variable "subnet_ids" {
  type    = list(string)
  default = []
}

variable "python_image" {
  type    = string
  default = "000000000000.dkr.ecr.us-west-2.amazonaws.com/hcd/python-aws:dev"
}

variable "serving_bucket" {
  type    = string
  default = "hcd-serving-placeholder"
}

variable "callback_url" {
  type    = string
  default = "http://localhost/callback"
}
