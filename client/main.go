package client

import (
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/pretty"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	initialBackoffMS  = 1000
	maxBackoffMS      = 32000
	backoffFactor     = 2
	rateLimitHttpCode = 429
)

// NewClient will initialize and return an authorized Graph Client
func NewClient(tenantId, clientId, clientSecret string) (*GraphClient, error) {
	// Initialize client
	graphClient := initClient(tenantId, clientId, clientSecret)

	// Login
	err := graphClient.login()

	// Handle error
	if err != nil {
		return nil, err
	}

	return graphClient, nil
}

// initClient will initialize and return a new Graph Client
func initClient(tenantId, clientId, clientSecret string) *GraphClient {
	return &GraphClient{
		TenantId:     tenantId,
		ClientId:     clientId,
		ClientSecret: clientSecret,
		AccessToken:  "",
		httpClient: &http.Client{
			Timeout: time.Second * 10,
		},
	}
}

// login will get a JWT with the correct grant type for collecting logs
func (graphClient *GraphClient) login() error {
	params := url.Values{}
	params.Set("scope", "https://graph.microsoft.com/.default")
	params.Set("client_id", graphClient.ClientId)
	params.Set("client_secret", graphClient.ClientSecret)
	params.Set("grant_type", "client_credentials")
	body, err := graphClient.conductRequestRaw("POST", fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", graphClient.TenantId), params, "application/x-www-form-urlencoded")

	// Handle errors
	if err != nil {
		return errors.New(string(body))
	}

	// Unmarshal response json
	var authResponse GraphAuthResponse
	err = json.Unmarshal(body, &authResponse)

	// Handle error
	if err != nil {
		return errors.New(fmt.Sprintf("error on unmarshal response body: %v", err))
	}

	// Set access token
	graphClient.AccessToken = authResponse.AccessToken

	return nil
}

// GetAlerts will retrieve the events between the two supplied timestamps and send the results to the channel
func (graphClient *GraphClient) GetAlerts(lastPollTimestamp, currentTimestamp string, resultsChannel chan<- string) (int, error) {
	// Setup variable
	count := 0

	// Parse last poll timestamp
	lastPollTime, err := time.Parse(time.RFC3339, lastPollTimestamp)

	// Handle error
	if err != nil {
		return -1, err
	}

	// Set up gt filter param string
	gtTime := lastPollTime.UTC().Format("2006-01-02T15:04:05Z")

	// Parse current poll timestamp
	currentPollTime, err := time.Parse(time.RFC3339, currentTimestamp)

	// Handle error
	if err != nil {
		return -1, err
	}

	// Set up le filter param string
	leTime := currentPollTime.UTC().Format("2006-01-02T15:04:05Z")

	// Set up parameters
	params := url.Values{}
	params.Set("$filter", "createdDateTime gt "+gtTime+" and createdDateTime le "+leTime)

	// Conduct request
	body, err := graphClient.conductRequest("GET", "https://graph.microsoft.com/v1.0/security/alerts", params)

	// Handle error
	if err != nil {
		return -1, err
	}

	// Parse Graph Security Alerts
	var response GraphSecurityAlertsResponse
	err = json.Unmarshal(body, &response)

	// Handle error
	if err != nil {
		return -1, err
	}

	// Handle empty responses
	if len(response.Value) == 0 {
		return 0, nil
	} else {
		// Convert results to array of strings
		data := convertInterfaceToString(response.Value)

		// Add current data count
		count += len(data)

		// Send events to results channel
		for _, event := range data {
			resultsChannel <- string(pretty.Ugly([]byte(event)))
		}
	}

	// Print number of results
	log.Debugf("Response had %v values", len(response.Value))

	// While response next link is not empty, do loop
	for response.NextLink != "" {
		// Get next link
		tmpNextLink := response.NextLink

		// Parse next link
		nextLink, err := url.Parse(response.NextLink)

		// Handle error
		if err != nil {
			return -1, err
		}

		// Parse query params
		nextLinkParams, _ := url.ParseQuery(nextLink.RawQuery)

		// Get skip token
		skipToken := nextLinkParams.Get("$skiptoken")

		// Set skip token
		params.Set("$skiptoken", skipToken)

		// Do request
		body, err = graphClient.conductRequest("GET", "https://graph.microsoft.com/v1.0/security/alerts", params)

		// Handle error
		if err != nil {
			return -1, err
		}

		// Unmarshal json
		err = json.Unmarshal(body, &response)

		// Handle error
		if err != nil {
			return -1, err
		}

		// Handle empty responses
		if len(response.Value) == 0 {
			return 0, nil
		} else {
			// Convert results to array of strings
			data := convertInterfaceToString(response.Value)

			// Add current data count
			count += len(data)

			// Send events to results channel
			for _, event := range data {
				resultsChannel <- string(pretty.Ugly([]byte(event)))
			}
		}

		// Handle results
		log.Debugf("Response had %v number of results", len(response.Value))

		// Break look if the next link is equal last next link
		if tmpNextLink == response.NextLink {
			break
		}
	}

	return count, nil
}

// conductRequest conducts a json request
func (graphClient *GraphClient) conductRequest(method string, uri string, params url.Values) ([]byte, error) {
	return graphClient.conductRequestRaw(method, uri, params, "application/json")
}

// conductRequestRaw will build the correct request and handle any errors
func (graphClient *GraphClient) conductRequestRaw(method string, uri string, params url.Values, contentType string) ([]byte, error) {
	// Build the URL
	aptUrl, err := url.Parse(uri)

	if err != nil {
		log.Debugf("error during URI parsing: %v", err)
		return nil, err
	}

	// Setup headers
	headers := make(map[string]string)
	headers["Accept"] = "*/*"
	headers["Content-Type"] = contentType

	// Convert method to uppercase
	method = strings.ToUpper(method)

	// JSON marshal body
	var requestBody string = ""

	// Encode params if GET request
	if method == "GET" {
		aptUrl.RawQuery = params.Encode()
	} else if method == "POST" || method == "PUT" {
		if contentType == "application/x-www-form-urlencoded" {
			requestBody = params.Encode()
		} else {
			bodyString, _ := json.Marshal(params)
			requestBody = string(bodyString)
		}
	}

	// Print calling url
	log.Debugf("calling URL: %s", aptUrl.String())

	// Make retryable HTTP call
	_, body, err := graphClient.makeRetryableHttpCall(method, *aptUrl, headers, requestBody)

	// Handle errors
	if err != nil {
		return body, err
	}

	return body, nil
}

// makeRetryableHttpCall will conduct an HTTP request and handle retries with backoff for rate limit responses
func (graphClient *GraphClient) makeRetryableHttpCall(
	method string,
	urlObj url.URL,
	headers map[string]string,
	body string,
) (*http.Response, []byte, error) {
	backoffMs := initialBackoffMS
	for {
		var request *http.Request
		var err error

		// Setup body if provided
		if body == "" {
			request, err = http.NewRequest(method, urlObj.String(), nil)
		} else {
			request, err = http.NewRequest(method, urlObj.String(), strings.NewReader(body))
		}

		// Handle errors
		if err != nil {
			return nil, nil, err
		}

		// Setup headers
		if headers != nil {
			for k, v := range headers {
				request.Header.Set(k, v)
			}
		}

		// Set access token if exists
		if graphClient.AccessToken != "" {
			request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", graphClient.AccessToken))
		}

		// Conduct request
		resp, err := graphClient.httpClient.Do(request)
		var body []byte

		// Return non 200 and non rate limit responses
		if err != nil || (resp.StatusCode != 200 && resp.StatusCode != rateLimitHttpCode) {
			// Warn on 206 Partial Content
			if resp.StatusCode == 206 {
				log.Warnf("header present - `Warning: %v`", resp.Header.Get("Warning"))
				log.Warnf("this means that a MS provider returned an error code")
				log.Warnf("see: https://docs.microsoft.com/en-us/graph/api/resources/security-error-codes?view=graph-rest-1.0")
			}

			body, err = ioutil.ReadAll(resp.Body)
			resp.Body.Close()

			if err == nil {
				return resp, body, errors.New(resp.Status)
			}
			return resp, body, err
		}

		// Handle backup or non rate limit
		if backoffMs > maxBackoffMS || resp.StatusCode != rateLimitHttpCode {
			body, err = ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			return resp, body, err
		}

		// Sleep to retry due to rate limit response
		time.Sleep(time.Millisecond * time.Duration(backoffMs))
		backoffMs *= backoffFactor
	}
}

func convertInterfaceToString(items []interface{}) []string {
	var data []string
	for _, val := range items {
		// Convert item to json byte array
		plain, _ := json.Marshal(val)

		// Add string to array
		data = append(data, string(plain))
	}

	return data
}
