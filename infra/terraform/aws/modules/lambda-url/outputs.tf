output "function_name" {
  value = aws_lambda_function.python.function_name
}

output "function_arn" {
  value = aws_lambda_function.python.arn
}

output "function_version" {
  value = aws_lambda_function.python.version
}

output "blue_alias_arn" {
  value = aws_lambda_alias.color["blue"].arn
}

output "green_alias_arn" {
  value = aws_lambda_alias.color["green"].arn
}

output "blue_function_url" {
  description = "HTTPS endpoint for the blue alias."
  value       = aws_lambda_function_url.color["blue"].function_url
}

output "green_function_url" {
  description = "HTTPS endpoint for the green alias."
  value       = aws_lambda_function_url.color["green"].function_url
}
