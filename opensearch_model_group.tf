resource "opensearch_model_group" "test" {
  provider = skpr-opensearch

  name        = "test"
  description = "A test model group"
}
