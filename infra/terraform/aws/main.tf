data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "hcd-python-lambda"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "lambda_logs" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_s3_bucket" "serving" {
  bucket = var.serving_bucket
}

resource "aws_dynamodb_table" "prefs" {
  name         = "hcd-prefs"
  billing_mode = "PROVISIONED"
  hash_key     = "user_sub"

  read_capacity  = 1
  write_capacity = 1

  attribute {
    name = "user_sub"
    type = "S"
  }
}

data "aws_iam_policy_document" "lambda_data" {
  statement {
    sid = "ServingObjects"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
    ]
    resources = ["${aws_s3_bucket.serving.arn}/*"]
  }

  statement {
    sid = "ServingBucket"
    actions = [
      "s3:ListBucket",
      "s3:GetBucketLocation",
    ]
    resources = [aws_s3_bucket.serving.arn]
  }

  statement {
    sid = "Prefs"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:DescribeTable",
    ]
    resources = [aws_dynamodb_table.prefs.arn]
  }
}

resource "aws_iam_role_policy" "lambda_data" {
  name   = "hcd-python-data"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_data.json
}

module "python_lambda" {
  source = "./modules/lambda-url"

  image_uri                      = var.python_image
  role_arn                       = aws_iam_role.lambda.arn
  reserved_concurrent_executions = var.reserved_concurrent_executions
  blue_version                   = var.blue_version
  green_version                  = var.green_version
  environment = {
    APP_BACKEND = "aws-python"
    APP_LANG    = "python"
    APP_RUNTIME = "lambda"
    S3_BUCKET   = var.serving_bucket
    PREFS_TABLE = "hcd-prefs"
  }
}

resource "aws_cognito_user_pool" "hcd" {
  name = "hcd"
}

resource "aws_cognito_user_pool_client" "spa" {
  name                                 = "hcd-spa"
  user_pool_id                         = aws_cognito_user_pool.hcd.id
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "email"]
  allowed_oauth_flows_user_pool_client = true
  callback_urls                        = [var.callback_url]
  supported_identity_providers         = ["COGNITO"]
  generate_secret                      = false
}

resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

data "aws_iam_policy_document" "github" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }
    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["repo:ram290476/hybrid-cloud-dashboard:*"]
    }
  }
}

resource "aws_iam_role" "github" {
  name               = "hcd-github-actions"
  assume_role_policy = data.aws_iam_policy_document.github.json
}
