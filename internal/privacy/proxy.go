package privacy

import (
	"io"
	"net/http"
)


func FetchMediaProxied(remoteURL string) (io.ReadCloser, string, error) {
	req, err := http.NewRequest("GET", remoteURL, nil)
	if err != nil {
		return nil, "", err
	}

	
	req.Header.Set("User-Agent", "Fedinet-Privacy-Proxy/1.0")
	req.Header.Del("Referer")
	req.Header.Del("X-Forwarded-For")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}

	return resp.Body, resp.Header.Get("Content-Type"), nil
}
