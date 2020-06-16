package couchbase

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/go-version"
	"io/ioutil"
	"net/http"
)

func CheckForOldCouchbaseVersion(hostname, username, password string) (is_old bool, err error) {

	//[TODO] handle list of hostnames

	resp, err := http.Get(fmt.Sprintf("http://%s:%s@%s:8091/pools", username, password, hostname))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	type Pools struct {
		ImplementationVersion string `json:"implementationVersion"`
	}
	data := Pools{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		return false, err
	}
	v, err := version.NewVersion(data.ImplementationVersion)

	v650, err := version.NewVersion("6.5.0-0000")
	if err != nil {
		return false, err
	}

	if v.LessThan(v650) {
		return true, nil
	}
	return false, nil

}

func getRootCAfromCouchbase(url string) (Base64pemCA string, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(body), nil
}
