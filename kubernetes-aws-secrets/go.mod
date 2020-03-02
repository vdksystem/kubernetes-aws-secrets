module github.com/vdksystem/kubernetes-aws-secrets

go 1.13

require (
	github.com/aws/aws-lambda-go v1.14.0
	github.com/aws/aws-sdk-go v1.29.8
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	k8s.io/api v0.17.3
	k8s.io/apimachinery v0.17.3
	k8s.io/client-go v0.17.0
	k8s.io/utils v0.0.0-20200124190032-861946025e34 // indirect
	sigs.k8s.io/aws-iam-authenticator v0.5.0
)
