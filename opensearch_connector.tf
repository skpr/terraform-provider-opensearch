resource "opensearch_connector" "test" {
  provider = skpr-opensearch

  body = jsonencode({
    name        = "Amazon Bedrock Connector: embedding"
    description = "The connector to bedrock Titan embedding model"
    version     = 1
    protocol    = "aws_sigv4"

    parameters = {
      region         = "ap-southeast-2"
      service_name   = "bedrock"
      model          = "amazon.titan-embed-text-v2:0"
      dimensions     = 1024
      normalize      = true
      embeddingTypes = ["float"]
    }

    credential = {
      access_key = "XXXX"
      secret_key = "XXXX"
    }

    actions = [
      {
        action_type = "predict"
        method      = "POST"
        url         = "https://bedrock-runtime.$${parameters.region}.amazonaws.com/model/$${parameters.model}/invoke"

        headers = {
          content-type           = "application/json"
          x-amz-content-sha256   = "required"
        }

        request_body = "{ \"inputText\": \"$${parameters.inputText}\", \"dimensions\": $${parameters.dimensions}, \"normalize\": $${parameters.normalize}, \"embeddingTypes\": $${parameters.embeddingTypes} }"

        pre_process_function  = "connector.pre_process.bedrock.embedding"
        post_process_function = "connector.post_process.bedrock.embedding"
      }
    ]
  })
}
