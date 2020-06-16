package couchbase

import (
	"context"
	"fmt"
	"sync"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"time"

	"github.com/hashicorp/vault/sdk/database/helper/connutil"
	"github.com/couchbase/gocb/v2"
	"github.com/hashicorp/errwrap"
	"github.com/mitchellh/mapstructure"
)

type couchbaseDBConnectionProducer struct {
	PublicKey    string `json:"public_key" structs:"public_key" mapstructure:"public_key"`
	PrivateKey   string `json:"private_key" structs:"private_key" mapstructure:"private_key"`
	ProjectID    string `json:"project_id" structs:"project_id" mapstructure:"project_id"`
	Hosts        string `json:"hosts" structs:"hosts" mapstructure:"hosts"`
	Port         int    `json:"port" structs:"port" mapstructure:"port"`
	Username     string `json:"username" structs:"username" mapstructure:"username"`
	Password     string `json:"password"   structs:"password" mapstructure:"password"`
	TLS          bool   `json:"tls" structs:"tls" mapstructure:"tls"`
	Insecure_TLS bool   `json:"insecure_tls" structs:"insecure_tls" mapstructure:"insecure_tls"`
	Base64Pem    string `json:"base64pem" structs:"base64pem" mapstructure:"base64pem"`
	Bucket_name  string `json:"bucket_name" structs:bucket_name" mapstructure:bucket_name"`

	Initialized bool
	rawConfig   map[string]interface{}
	Type        string
	cluster     *gocb.Cluster
	sync.Mutex
}

func (c *couchbaseDBConnectionProducer) secretValues() map[string]interface{} {
	return map[string]interface{}{
		c.Password: "[password]",
		c.Username: "[username]",
	}
}

func (c *couchbaseDBConnectionProducer) Init(ctx context.Context, config map[string]interface{}, verifyConnection bool) (saveConfig map[string]interface{}, err error) {

	c.Lock()
	defer c.Unlock()

	c.rawConfig = config

	err = mapstructure.WeakDecode(config, c)
	if err != nil {
		return nil, err
	}

	switch {
	case len(c.Hosts) == 0:
		return nil, fmt.Errorf("hosts cannot be empty")
	case len(c.Username) == 0:
		return nil, fmt.Errorf("username cannot be empty")
	case len(c.Password) == 0:
		return nil, fmt.Errorf("password cannot be empty")
	}

	if c.TLS {
		if len(c.Base64Pem) == 0 {
			return nil, fmt.Errorf("base64pem cannot be empty")
		}

		if !strings.HasPrefix(c.Hosts, "couchbases://") {
			return nil, fmt.Errorf("hosts list must start with couchbases:// for TLS connection")
		}
	}
	
	c.Initialized = true

	if verifyConnection {
		if _, err := c.Connection(ctx); err != nil {
			return nil, errwrap.Wrapf("error verifying connection: {{err}}", err)
		}
	}

	return config, nil
}

func (c *couchbaseDBConnectionProducer) Initialize(ctx context.Context, config map[string]interface{}, verifyConnection bool) error {
	_, err := c.Init(ctx, config, verifyConnection)
	return err
}
func (c *couchbaseDBConnectionProducer) Connection(_ context.Context) (interface{}, error) {
	// This is intentionally not grabbing the lock since the calling functions (e.g. CreateUser)
	// are claiming it. (The locking patterns could be refactored to be more consistent/clear.)

	if !c.Initialized {
		return nil, connutil.ErrNotInitialized
	}

	if c.cluster != nil {
		return c.cluster, nil
	}
	var err error
	var sec gocb.SecurityConfig
	var PEM []byte

	if c.TLS {
		PEM, err = base64.StdEncoding.DecodeString(c.Base64Pem)
		if err != nil {
			return nil, errwrap.Wrapf("error decoding Base64Pem: {{err}}", err)
		}
		rootCAs := x509.NewCertPool()
		ok := rootCAs.AppendCertsFromPEM([]byte(PEM))
		if !ok {
			return nil, fmt.Errorf("Failed to parse root certificate")
		}
		sec = gocb.SecurityConfig{
			TLSRootCAs:    rootCAs,
			TLSSkipVerify: c.Insecure_TLS,
		}
	}

	c.cluster, err = gocb.Connect(
		c.Hosts,
		gocb.ClusterOptions{
			Username:       c.Username,
			Password:       c.Password,
			SecurityConfig: sec,
		})
	if err != nil {
		return nil, errwrap.Wrapf("error in Connection: {{err}}", err)
	}

	// For databases 6.0 and earlier, we will need to open a `Bucket instance before connecting to any other
	// HTTP services such as UserManager.

	if c.Bucket_name != "" {
		bucket := c.cluster.Bucket(c.Bucket_name)
		// We wait until the bucket is definitely connected and setup.
		err = bucket.WaitUntilReady(5*time.Second, nil)
		if err != nil {
			return nil, errwrap.Wrapf("error in Connection waiting for bucket: {{err}}", err)
		}
	} else {
		err = c.cluster.WaitUntilReady(5*time.Second, nil)
		
		if err != nil {
			//s := fmt.Sprintf("Error, user %#v, error {{err}}", c)
			//return nil, errwrap.Wrapf(s, err)
			return nil, errwrap.Wrapf("error in Connection waiting for cluster: {{err}}", err)
		}
	}
	
	return c.cluster, nil
}

// close terminates the database connection without locking
func (c *couchbaseDBConnectionProducer) close() error {

	if c.cluster != nil {
		if err := c.cluster.Close(&gocb.ClusterCloseOptions{}); err != nil {
			return err
		}
	}

	c.cluster = nil
	return nil
}

// Close terminates the database connection with locking
func (c *couchbaseDBConnectionProducer) Close() error {
	c.Lock()
	defer c.Unlock()

	return c.close()
}
