package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
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
	labels      map[string]string
	annotations map[string]string
	stringData  map[string]string
}

func LambdaHandler(ctx context.Context, secretName string) error {
	log.Printf("Got update event for %s", secretName)

	awsSecret := getAwsSecret(secretName)

	region := os.Getenv("Region")
	if strings.TrimSpace(region) == "" {
		region = os.Getenv("AWS_REGION")
	}

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region)},
	)
	checkError(err)

	clusterId := os.Getenv("ClusterId")
	k8s := getK8sClientSet(clusterId, sess)
	secret, err := k8s.CoreV1().Secrets(awsSecret.namespace).Get(awsSecret.name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			createSecret(awsSecret, k8s)
			return nil
		}
		log.Fatal(err)
	}
	updateSecret(awsSecret, secret, k8s)
	return nil
}

func getAwsSecret(name string) *awsSecret {
	var secret = new(awsSecret)

	// We assume that lambda is located in the same region with secrets
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(os.Getenv("AWS_REGION"))},
	)
	secrets := secretsmanager.New(sess)

	input := &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String(name),
		VersionStage: aws.String("AWSCURRENT"), // VersionStage defaults to AWSCURRENT if unspecified
	}

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
		stringData[k] = v.(string)
	}

	var clusters []string
	regCluster, err := regexp.Compile("^kubernetes.io/cluster/(.*)")
	checkError(err)

	var labels = make(map[string]string)
	regLabel, err := regexp.Compile("^label/(.*)")
	checkError(err)

	var annotations = make(map[string]string)
	regAn, err := regexp.Compile("^annotation/(.*)")
	checkError(err)

	tags := getSecretTags(name, secrets)
	for _, t := range tags {
		if regLabel.MatchString(*t.Key) {
			label := regLabel.FindStringSubmatch(*t.Key)[1]
			labels[label] = *t.Value
		}
		if regAn.MatchString(*t.Key) {
			annottation := regAn.FindStringSubmatch(*t.Key)[1]
			annotations[annottation] = *t.Value
		}
		if regCluster.MatchString(*t.Key) {
			cluster := regCluster.FindStringSubmatch(*t.Key)[1]
			clusters = append(clusters, cluster)
		}
	}

	secret.labels = labels
	secret.annotations = annotations
	secret.stringData = stringData
	secret.name = strings.SplitN(name, "/", 2)[1]
	secret.namespace = strings.SplitN(name, "/", 2)[0]

	return secret
}

func getSecretTags(secretId string, secrets *secretsmanager.SecretsManager) []*secretsmanager.Tag {
	input := &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(secretId),
	}

	output, err := secrets.DescribeSecret(input)
	checkError(err)

	return output.Tags
}

func getK8sClientSet(clusterID string, s *session.Session) *kubernetes.Clientset {
	eksCluster := eks.New(s)

	input := eks.DescribeClusterInput{Name: &clusterID}
	output, err := eksCluster.DescribeCluster(&input)
	checkError(err)

	k8sToken := getToken(clusterID)

	caData, _ := base64.StdEncoding.DecodeString(*output.Cluster.CertificateAuthority.Data)
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
		ClusterID: clusterID,
		Region:    os.Getenv("AWS_REGION"),
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
		log.Fatal(err)
	}
}
func main() {
	//TODO filter by cluster
	lambda.Start(LambdaHandler)
	//_ = LambdaHandler(context.Background(), "devops/lambdatest")
}
