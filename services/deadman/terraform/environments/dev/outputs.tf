# Outputs

output "service_url" {
  description = "Cloud Run service URL (configure as Twilio webhook)"
  value       = module.cloud_run.service_url
}

output "service_name" {
  description = "Cloud Run service name"
  value       = module.cloud_run.service_name
}

output "twilio_webhook_url" {
  description = "Full Twilio webhook URL to configure at https://console.twilio.com"
  value       = "${module.cloud_run.service_url}/sms"
}

output "cloud_sql_connection_name" {
  description = "Cloud SQL connection name (project:region:instance)"
  value       = module.cloud_sql.connection_name
}

output "custom_domain_url" {
  description = "Public URL via custom domain"
  value       = "https://${var.custom_domain}"
}
