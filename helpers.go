package oauth2

import (
	"encoding/json"
	"net/http"
	"net/url"
)

func WriteJSON(w http.ResponseWriter, doc interface{}, status int) error {
	// set required headers
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	// set status
	w.WriteHeader(status)

	// write error document
	return json.NewEncoder(w).Encode(doc)
}

func WriteRedirect(w http.ResponseWriter, uri string, params map[string]string) error {
	// parse redirect uri
	redirectURI, err := url.Parse(uri)
	if err != nil {
		return err
	}

	// get current query
	q := redirectURI.Query()

	// add parameters
	for k, v := range params {
		q.Add(k, v)
	}

	// reset query
	redirectURI.RawQuery = q.Encode()

	// set location
	w.Header().Add("Location", redirectURI.String())

	// write redirect
	w.WriteHeader(http.StatusFound)

	// finish response
	_, err = w.Write(nil)
	return err
}
