package rancher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
)

type Client struct {
	rootUrl url.URL
	client  http.Client
}

func New(rootUrl url.URL) Client {
	return Client{rootUrl, http.Client{}}
}

func (c *Client) Get(path string, result interface{}) error {
	return c.do("GET", path, nil, result)
}

func (c *Client) Put(path string, data, result interface{}) error {
	var body *bytes.Buffer
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("error marshaling body: {}", err)
		}
		body = bytes.NewBuffer(b)
	}
	return c.do("PUT", path, body, result)
}

func (c *Client) do(method, path string, body io.Reader, result interface{}) error {
	url := c.rootUrl
	url.Path += path
	req, err := http.NewRequest(method, url.String(), body)
	if err != nil {
		return err
	}
	user := c.rootUrl.User
	if user == nil {
		return fmt.Errorf("user and password must be set in url")
	}
	password, isSet := user.Password()
	if !isSet {
		return fmt.Errorf("password must be set in url")
	}
	req.SetBasicAuth(user.Username(), password)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("Expected to API to return 200, got %s: %s",
			resp.Status,
			respBody)
	}
	if result != nil {
		return json.Unmarshal(respBody, result)
	}
	return nil
}
