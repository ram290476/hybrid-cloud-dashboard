# Documented alternative to ECS Fargate. The AWS root does not call this module.
# Keep the Python app container-first so either runtime can ship the same image.
variable "image_uri" {
  type = string
}

variable "role_arn" {
  type = string
}

resource "aws_lambda_function" "python" {
  function_name = "py-api"
  package_type  = "Image"
  image_uri     = var.image_uri
  role          = var.role_arn
  architectures = ["arm64"]
  memory_size   = 512
  timeout       = 10
}

resource "aws_lambda_function_url" "python" {
  function_name      = aws_lambda_function.python.function_name
  authorization_type = "NONE"
}
