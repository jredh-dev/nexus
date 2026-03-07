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
