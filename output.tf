output "model_group_id" {
  value = opensearch_model_group.test.id
}

output "connector_id" {
  value = opensearch_connector.test.id
}

output "model_register_id" {
  value = opensearch_model_register.huggingface_msmarco_distilbert.model_id
}
