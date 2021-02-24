package sf

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/cloudflare/cloudflare-go"
	log "github.com/sirupsen/logrus"
)

// TraefikRouter is a struct to store a router object of Traefik
type TraefikRouter struct {
	EntryPoints []string `json:"entryPoints"`
	Middlewares []string `json:"middlewares,omitempty"`
	Service     string   `json:"service"`
	Rule        string   `json:"rule"`
	TLS         struct {
		CertResolver string `json:"certResolver"`
		Domains      []struct {
			Main string   `json:"main"`
			Sans []string `json:"sans"`
		} `json:"domains"`
	} `json:"tls,omitempty"`
	Status   string   `json:"status"`
	Using    []string `json:"using"`
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	Priority int64    `json:"priority,omitempty"`
}

func checkDuplicateRule(rule string, rules []cloudflare.DNSRecord) bool {
	for _, r := range rules {
		if r.Name == rule {
			return true
		}
	}
	return false
}

// GetTraefikRules gathers and formats all Traefik http routers
func GetTraefikRules(userRecords []cloudflare.DNSRecord) []cloudflare.DNSRecord {
	for _, traefikInstance := range config.TraefikInstances {
		traefikURL, err := url.Parse(traefikInstance.URL)
		if err != nil {
			log.Fatalf("Unable to parse Traefik url %s: %s", traefikInstance.URL, err)
		}
		traefikURL.Path = path.Join(traefikURL.Path, "/api/http/routers")
		traefikHost := traefikURL.String()

		client := &http.Client{}
		req, err := http.NewRequest("GET", traefikHost, nil)
		if err != nil {
			log.Fatalf("Error creating http request for Traefik instance %s: %s", traefikInstance.Name, err)
		}
		if traefikInstance.Username != "" && traefikInstance.Password != "" {
			req.SetBasicAuth(traefikInstance.Username, traefikInstance.Password)
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("Unable to get Traefik (%s) rules: %s", traefikInstance.Name, err)
		}
		if resp.StatusCode != 200 {
			log.Fatalf("Unable to get Traefik (%s) rules: http status code %d", traefikInstance.Name, resp.StatusCode)
		}

		respData, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("Unable to read Traefik (%s) rules: %s", traefikInstance.Name, err)
		}

		var traefikRouters []TraefikRouter
		err = json.Unmarshal(respData, &traefikRouters)
		if err != nil {
			log.Fatalf("Unable to load Traefik (%s) rules: %s", traefikInstance.Name, err)
		}

		var re = regexp.MustCompile(`(?m)Host\(\x60(([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,})\x60\)`)

		var content string
		switch config.Cloudflare.Defaults.Type {
		case "A":
			content = currentIP
		case "CNAME":
			content = config.Cloudflare.ZoneName
		}

		var ruleNames []string
	rules:
		for _, router := range traefikRouters {
			if re.MatchString(router.Rule) {
				match := re.FindStringSubmatch(router.Rule)[1]
				if !checkDuplicateRule(match, userRecords) {
					for _, ignoredRule := range traefikInstance.IgnoredRules {
						if strings.Contains(match, ignoredRule) {
							continue rules
						}
					}
					userRecords = append(userRecords, cloudflare.DNSRecord{
						Type:    config.Cloudflare.Defaults.Type,
						Name:    match,
						Content: content,
						Proxied: config.Cloudflare.Defaults.Proxied,
						TTL:     config.Cloudflare.Defaults.TTL,
					})
					ruleNames = append(ruleNames, match)
				}
			}
		}
		log.Debugf("Found rules in Traefik instance %s: %s", traefikInstance.Name, strings.Join(ruleNames, ", "))
	}

	return userRecords
}
