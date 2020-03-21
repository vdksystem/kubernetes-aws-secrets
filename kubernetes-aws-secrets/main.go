package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"os"
	"regexp"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
	"strings"
)

type awsSecret struct {
	name        string
	namespace   string
	env         string
	labels      map[string]string
	annotations map[string]string
	stringData  map[string]string
}
type config struct {
	region  string
	role    string
	creds   *credentials.Credentials
	secrets *secretsmanager.SecretsManager
	eks     *eks.EKS
}

var cfg = new(config)

func LambdaHandler(ctx context.Context, secretName string) error {
	log.Printf("Got update event for %s", secretName)
	initConfig()

	awsSecret := getAwsSecret(secretName)

	clusterId := os.Getenv("ClusterId")
	k8s := getK8sClientSet(clusterId)
	secret, err := k8s.CoreV1().Secrets(awsSecret.namespace).Get(awsSecret.name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			createSecret(awsSecret, k8s)
			return nil
		}
		log.Fatalf("ERROR: %v", err)
	}
	updateSecret(awsSecret, secret, k8s)
	return nil
}

func getAwsSecret(name string) *awsSecret {
	var secret = new(awsSecret)

	input := &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String(name),
		VersionStage: aws.String("AWSCURRENT"), // VersionStage defaults to AWSCURRENT if unspecified
	}

	secrets := cfg.secrets
	result, err := secrets.GetSecretValue(input)
	checkError(err)

	var secretString string
	if result.SecretString != nil {
		secretString = *result.SecretString
	}

	var f interface{}
	_ = json.Unmarshal([]byte(secretString), &f)
	secretMap := f.(map[string]interface{})
	stringData := make(map[string]string)
	for k, v := range secretMap {
		sec := v.(string)
		stringData[k] = strings.TrimSpace(sec)
	}

	var clusters []string
	regCluster, err := regexp.Compile(`^kubernetes.io/cluster/(.*)`)
	checkError(err)

	var labels = make(map[string]string)
	regLabel, err := regexp.Compile(`^label/(.*)`)
	checkError(err)

	var annotations = make(map[string]string)
	regAn, err := regexp.Compile(`^annotation/(.*)`)
	checkError(err)

	tags := getSecretTags(name)
	for _, t := range tags {
		if regLabel.MatchString(*t.Key) {
			label := regLabel.FindStringSubmatch(*t.Key)[1]
			labels[label] = *t.Value
		}
		if regAn.MatchString(*t.Key) {
			annotation := regAn.FindStringSubmatch(*t.Key)[1]
			annotations[annotation] = *t.Value
		}
		if regCluster.MatchString(*t.Key) {
			cluster := regCluster.FindStringSubmatch(*t.Key)[1]
			clusters = append(clusters, cluster)
		}
	}

	secret.labels = labels
	secret.annotations = annotations
	secret.stringData = stringData
	secret.name = strings.SplitN(name, "/", 3)[2]
	secret.namespace = strings.SplitN(name, "/", 3)[1]
	secret.env = strings.SplitN(name, "/", 3)[0]

	return secret
}

func getSecretTags(secretId string) []*secretsmanager.Tag {
	input := &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(secretId),
	}

	secrets := cfg.secrets
	output, err := secrets.DescribeSecret(input)
	checkError(err)

	return output.Tags
}

func getK8sClientSet(clusterID string) *kubernetes.Clientset {
	eksCluster := cfg.eks
	input := eks.DescribeClusterInput{Name: &clusterID}
	output, err := eksCluster.DescribeCluster(&input)
	checkError(err)
	caData, _ := base64.StdEncoding.DecodeString(*output.Cluster.CertificateAuthority.Data)

	k8sToken := getToken(clusterID)

	config := rest.Config{
		Host:            *output.Cluster.Endpoint,
		BearerToken:     k8sToken.Token,
		TLSClientConfig: rest.TLSClientConfig{CAData: caData},
	}

	client, err := kubernetes.NewForConfig(&config)
	checkError(err)

	return client
}

func getToken(clusterID string) token.Token {
	var tok token.Token
	gen, err := token.NewGenerator(false, false)
	checkError(err)

	tok, err = gen.GetWithOptions(&token.GetTokenOptions{
		ClusterID:     clusterID,
		Region:        cfg.region,
		AssumeRoleARN: cfg.role,
	})
	checkError(err)

	return tok
}

func createSecret(awsSecret *awsSecret, k8s *kubernetes.Clientset) {
	secret := v1.Secret{}
	secret.Name = awsSecret.name
	secret.StringData = awsSecret.stringData
	secret.Labels = awsSecret.labels
	secret.Annotations = awsSecret.annotations
	res, err := k8s.CoreV1().Secrets(awsSecret.namespace).Create(&secret)
	checkError(err)
	log.Printf("Successfully created secret %s, namespace %s", res.Name, res.Namespace)
}

func updateSecret(awsSecret *awsSecret, secret *v1.Secret, k8s *kubernetes.Clientset) {
	secret.Name = awsSecret.name
	secret.StringData = awsSecret.stringData
	secret.Labels = awsSecret.labels
	secret.Annotations = awsSecret.annotations
	res, err := k8s.CoreV1().Secrets(secret.Namespace).Update(secret)
	checkError(err)
	log.Printf("Successfully updated secret %s, namespace %s", res.Name, res.Namespace)
}

func checkError(err error) {
	if err != nil {
		log.Fatalf("ERROR: %v", err)
	}
}

func initConfig() {
	// We assume that lambda is located in the same region with secrets
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(os.Getenv("AWS_REGION"))},
	)
	checkError(err)
	cfg.secrets = secretsmanager.New(sess)

	if strings.TrimSpace(os.Getenv("EKSRegion")) == "" {
		cfg.region = os.Getenv("AWS_REGION")
	} else {
		cfg.region = os.Getenv("EKSRegion")
	}
	cfg.role = os.Getenv("Role")

	eksSession := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(cfg.region)},
	))
	cfg.creds = stscreds.NewCredentials(eksSession, cfg.role)

	cfg.eks = eks.New(eksSession, &aws.Config{
		Credentials: cfg.creds,
		Region:      &cfg.region,
	})
}
func main() {
	lambda.Start(LambdaHandler)
	//_ = LambdaHandler(context.Background(), "ci/devops/lambdatest")
}
