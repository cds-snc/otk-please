package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/handlerfunc"
	"github.com/pkg/errors"
	"github.com/slack-go/slack"
)

// Response is of type APIGatewayProxyResponse since we're leveraging the
// AWS Lambda Proxy Request functionality (default behavior)
//
// https://serverless.com/framework/docs/providers/aws/events/apigateway/#lambda-proxy-integration
type Response events.APIGatewayProxyResponse

const (
	demoAddress    string = "https://submission.covid-alert-demo.cdssandbox.xyz/new-key-claim"
	stagingAddress string = "https://submission.wild-samphire.cdssandbox.xyz/new-key-claim"
)

func verifyRequest(req *http.Request) error {
	secretVerifier, err := slack.NewSecretsVerifier(req.Header, os.Getenv("SLACK_SIGNING_SECRET"))
	if err != nil {
		return errors.Wrap(err, "NewSecretsVerifier failed")
	}

	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return errors.Wrap(err, "ReadAll failed")
	}

	// we need to reset the body to avoid unexpected side effects
	req.Body = ioutil.NopCloser(bytes.NewBuffer(body))

	_, err = secretVerifier.Write(body)
	if err != nil {
		return errors.Wrap(err, "Ensure failed")
	}

	err = secretVerifier.Ensure()
	if err != nil {
		return errors.Wrap(err, "Ensure failed")
	}

	return nil
}

func getToken(bearerToken string, address string) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("POST", address, nil)
	if err != nil {
		return "", err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %v", bearerToken))

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSuffix(string(body), "\n"), nil
}

func handler(w http.ResponseWriter, req *http.Request) {
	if verifyRequest(req) != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	s, err := slack.SlashCommandParse(req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var (
		bearerToken string
		address     string
		environment string
	)

	text := s.Text
	demo := regexp.MustCompile("(?i)demo")
	staging := regexp.MustCompile("(?i)staging")

	if demo.MatchString(text) {
		bearerToken = os.Getenv("DEMO")
		address = demoAddress
		environment = "Demo"
	} else if staging.MatchString(text) {
		bearerToken = os.Getenv("STAGING")
		address = stagingAddress
		environment = "Staging"
	} else {
		w.Write([]byte("Please enter either *demo* or *staging*"))
		return
	}

	token, err := getToken(bearerToken, address)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write([]byte(fmt.Sprintf("%v token: %v", environment, token)))
}

var handlerFuncLambda *handlerfunc.HandlerFuncAdapter

func init() {
	handlerFuncLambda = handlerfunc.New(handler)
}

// Handler foo
func Handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	return handlerFuncLambda.ProxyWithContext(ctx, req)
}

func main() {
	lambda.Start(Handler)
}
