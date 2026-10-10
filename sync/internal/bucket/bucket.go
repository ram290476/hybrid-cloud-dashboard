// Package bucket reads and writes the prefs JSON object each backend mirrors
// to its object store. Cloud KV (DynamoDB, Firestore, Cosmos) stays the
// request path; this package is the sync transport for those same documents.
package bucket

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Bucket is one cloud object container.
type Bucket interface {
	List(prefix string) ([]string, error)
	Get(key string) ([]byte, error)
	Put(key string, body []byte) error
}

// GCS talks to fake-gcs-server or the Cloud Storage JSON API.
type GCS struct {
	Host, Name string
	Client     *http.Client
}

func (g GCS) List(prefix string) ([]string, error) {
	endpoint := g.url("/storage/v1/b/" + url.PathEscape(g.Name) + "/o?prefix=" + url.QueryEscape(prefix))
	var page struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := g.do(http.MethodGet, endpoint, nil, &page); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		out = append(out, item.Name)
	}
	return out, nil
}

func (g GCS) Get(key string) ([]byte, error) {
	return g.bytes(http.MethodGet, g.url("/storage/v1/b/"+url.PathEscape(g.Name)+"/o/"+url.PathEscape(key)+"?alt=media"), nil)
}

func (g GCS) Put(key string, body []byte) error {
	endpoint := g.url("/upload/storage/v1/b/" + url.PathEscape(g.Name) + "/o?uploadType=media&name=" + url.QueryEscape(key))
	_, err := g.bytes(http.MethodPost, endpoint, body)
	return err
}

func (g GCS) url(path string) string {
	host := g.Host
	if !strings.HasPrefix(host, "http") {
		host = "http://" + host
	}
	return strings.TrimRight(host, "/") + path
}

func (g GCS) do(method, endpoint string, body []byte, dest any) error {
	raw, err := g.bytes(method, endpoint, body)
	if err != nil {
		return err
	}
	if dest == nil || len(raw) == 0 {
		return nil
	}
	return jsonUnmarshal(raw, dest)
}

func (g GCS) bytes(method, endpoint string, body []byte) ([]byte, error) {
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gcs %s %s: %d %s", method, endpoint, resp.StatusCode, raw)
	}
	return raw, nil
}

func (g GCS) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

// S3 is a path-style client for SeaweedFS or AWS. Credentials are the local
// Seaweed identity (minioadmin) or placeholders, never a real cloud key.
type S3 struct {
	Endpoint, BucketName, AccessKey, SecretKey, Region string
	Client                                             *http.Client
}

func (s S3) List(prefix string) ([]string, error) {
	endpoint := s.base() + "/?prefix=" + url.QueryEscape(prefix)
	raw, err := s.signed(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var page struct {
		Contents []struct {
			Key string `xml:"Key"`
		} `xml:"Contents"`
	}
	if err := xml.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(page.Contents))
	for _, item := range page.Contents {
		out = append(out, item.Key)
	}
	return out, nil
}

func (s S3) Get(key string) ([]byte, error) {
	return s.signed(http.MethodGet, s.base()+"/"+escapeKey(key), nil)
}

func (s S3) Put(key string, body []byte) error {
	_, err := s.signed(http.MethodPut, s.base()+"/"+escapeKey(key), body)
	return err
}

func (s S3) base() string {
	return strings.TrimRight(s.Endpoint, "/") + "/" + s.BucketName
}

func (s S3) signed(method, endpoint string, body []byte) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if body == nil {
		body = []byte{}
	}
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	payload := sha256.Sum256(body)
	payloadHex := hex.EncodeToString(payload[:])
	region := s.Region
	if region == "" {
		region = "us-east-1"
	}
	canonicalQuery := u.RawQuery
	headers := map[string]string{
		"host":                 u.Host,
		"x-amz-content-sha256": payloadHex,
		"x-amz-date":           amzDate,
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canon strings.Builder
	canon.WriteString(method + "\n" + u.EscapedPath() + "\n" + canonicalQuery + "\n")
	for _, name := range names {
		canon.WriteString(name + ":" + headers[name] + "\n")
	}
	canon.WriteString("\n" + strings.Join(names, ";") + "\n" + payloadHex)
	scope := day + "/" + region + "/s3/aws4_request"
	hash := sha256.Sum256([]byte(canon.String()))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(hash[:])
	kDate := hmacSHA([]byte("AWS4"+s.SecretKey), day)
	kRegion := hmacSHA(kDate, region)
	kService := hmacSHA(kRegion, "s3")
	kSigning := hmacSHA(kService, "aws4_request")
	sig := hex.EncodeToString(hmacSHA(kSigning, toSign))
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHex)
	req.Header.Set("authorization", "AWS4-HMAC-SHA256 Credential="+s.AccessKey+"/"+scope+", SignedHeaders="+strings.Join(names, ";")+", Signature="+sig)
	return do(s.Client, req)
}

// Azure is a blob container client for Azurite. The account key is the public
// Azurite well-known key, or a placeholder in cloud config.
type Azure struct {
	Endpoint, Account, Key, Container string
	Client                            *http.Client
}

func (a Azure) List(prefix string) ([]string, error) {
	endpoint := a.base() + "?restype=container&comp=list&prefix=" + url.QueryEscape(prefix)
	raw, err := a.signed(http.MethodGet, endpoint, nil, "comp:list\nprefix:"+prefix+"\nrestype:container")
	if err != nil {
		return nil, err
	}
	var page struct {
		Blobs []struct {
			Name string `xml:"Name"`
		} `xml:"Blobs>Blob"`
	}
	if err := xml.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(page.Blobs))
	for _, blob := range page.Blobs {
		out = append(out, blob.Name)
	}
	return out, nil
}

func (a Azure) Get(key string) ([]byte, error) {
	return a.signed(http.MethodGet, a.base()+"/"+escapeKey(key), nil, "")
}

func (a Azure) Put(key string, body []byte) error {
	_, err := a.signed(http.MethodPut, a.base()+"/"+escapeKey(key), body, "")
	return err
}

func (a Azure) base() string {
	return strings.TrimRight(a.Endpoint, "/") + "/" + a.Account + "/" + a.Container
}

func (a Azure) signed(method, endpoint string, body []byte, extra string) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if body == nil {
		body = []byte{}
	}
	date := time.Now().UTC().Format(http.TimeFormat)
	length := ""
	if method == http.MethodPut {
		length = fmt.Sprintf("%d", len(body))
	}
	resource := "/" + a.Account + u.EscapedPath()
	if extra != "" {
		resource += "\n" + extra
	}
	toSign := method + "\n\n\n" + length + "\n\n\n\n\n\n\n\n\nx-ms-blob-type:BlockBlob\nx-ms-date:" + date + "\nx-ms-version:2020-10-02\n" + resource
	key, err := base64.StdEncoding.DecodeString(a.Key)
	if err != nil {
		return nil, err
	}
	sig := base64.StdEncoding.EncodeToString(hmacSHA(key, toSign))
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-ms-date", date)
	req.Header.Set("x-ms-version", "2020-10-02")
	req.Header.Set("x-ms-blob-type", "BlockBlob")
	req.Header.Set("authorization", "SharedKey "+a.Account+":"+sig)
	return do(a.Client, req)
}

func do(client *http.Client, req *http.Request) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %d %s", req.Method, req.URL.Path, resp.StatusCode, raw)
	}
	return raw, nil
}

func hmacSHA(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func jsonUnmarshal(raw []byte, dest any) error {
	// Avoid pulling encoding/json into the signature helpers' import block twice.
	return unmarshalJSON(raw, dest)
}
