# kubernetes-aws-secrets
This application is intended to sync secrets between AWS Secrets manager and kubernetes secrets

##How it works
Using SAM, we deploy Lambda function which is responsible for sync. 
Lambda is triggered by CloudWatch event. Event rule triggers Lambda on all events for aws.secretmanager source.

Because Lambda function uses aws IAM authentication, you have to add created IAM role to aws-auth ConfigMap (example in rbac folder)

Deploy this application in the account and region where AWS Secrets manager is located 