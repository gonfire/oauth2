package oauth2test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
)

func must(ok bool, msg string) {
	if !ok {
		panic(msg)
	}
}

func extend(src, ext map[string]string) map[string]string {
	ret := make(map[string]string)

	// add source keys
	for k, v := range src {
		ret[k] = v
	}

	// add extension keys
	for k, v := range ext {
		ret[k] = v
	}

	return ret
}

func jsonField(r *httptest.ResponseRecorder, field string) interface{} {
	srd := strings.NewReader(r.Body.String())
	dec := json.NewDecoder(srd)
	dst := make(map[string]interface{})

	err := dec.Decode(&dst)
	if err != nil {
		return nil
	}

	return dst[field]
}

func jsonFieldBool(r *httptest.ResponseRecorder, field string) bool {
	str, _ := jsonField(r, field).(bool)
	return str
}

func jsonFieldString(r *httptest.ResponseRecorder, field string) string {
	str, _ := jsonField(r, field).(string)
	return str
}

func jsonFieldFloat(r *httptest.ResponseRecorder, field string) float64 {
	num, _ := jsonField(r, field).(float64)
	return num
}

func fragment(r *httptest.ResponseRecorder, key string) string {
	u, err := url.Parse(r.Header().Get("Location"))
	if err != nil {
		panic(err)
	}

	f, err := url.ParseQuery(u.Fragment)
	if err != nil {
		panic(err)
	}

	return f.Get(key)
}

func query(r *httptest.ResponseRecorder, key string) string {
	u, err := url.Parse(r.Header().Get("Location"))
	if err != nil {
		panic(err)
	}

	return u.Query().Get(key)
}

func auth(r *httptest.ResponseRecorder, key string) string {
	header := r.Header().Get("WWW-Authenticate")
	parts := strings.SplitN(header, " ", 2)
	parts = strings.Split(parts[1], ", ")

	for _, part := range parts {
		values := strings.SplitN(part, "=", 2)
		if values[0] != key {
			continue
		}
		return strings.Trim(values[1], "\",")
	}

	return ""
}

func debug(rec *httptest.ResponseRecorder) string {
	return fmt.Sprintf("\nStatus: %d\nHeader: %v\nBody:   %v", rec.Code, rec.Header(), rec.Body.String())
}
