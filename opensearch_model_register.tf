resource "opensearch_model_register" "huggingface_msmarco_distilbert" {
  provider = skpr-opensearch

  body = jsonencode({
    name          = "bedrock titan embedding model v2"
    function_name = "remote"
    description   = "test embedding model"
    model_group_id = opensearch_model_group.test.id
    connector_id   = opensearch_connector.test.id
    model_format   = "TORCH_SCRIPT"

    model_config = {
      framework_type      = "sentence_transformers"
      model_type          = "TEXT_EMBEDDING"
      embedding_dimension = 1024

      additional_config = {
        space_type = "l2"
      }
    }
  })
}
