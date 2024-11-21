// Package githubversioncheck is used to determine if the currently run version of gophy matches the latest available github release
package githubversioncheck

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// getLatestGithubRelease checks what the latest gophy version on Github is, it is a bit hacky to avoid bearer token requirement
func getLatestGithubRelease(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("Failed to create the request: %v", err)
	}

	// send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Failed to send the request to GitHub: %v", err)
	}
	defer resp.Body.Close()

	// get data from response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Failed to read response body: %v", err)
	}

	// hacky way to determine currently newest gophy version from html response
	latestGophyVersion, err := extractVersionFromHtml(string(body))
	if err != nil || len(latestGophyVersion) < 2 {
		return "", err
	}

	return latestGophyVersion, nil
}

// extractVersionFromHtml is a hacky way to extract the gophy version from the html response
func extractVersionFromHtml(content string) (string, error) {
	// the pinnacle of coding
	start := "<title>Release "
	end := " ·"

	// get indices of start and end tags
	startIndex := strings.Index(content, start)
	if startIndex == -1 {
		return "", fmt.Errorf("Start location of <title> part was not found.")
	}
	endIndex := strings.Index(content, end)
	if endIndex == -1 {
		return "", fmt.Errorf("End of <title> part was not found.")
	}

	// extract substring between
	version := content[startIndex+len(start) : endIndex]

	// remove trailing "." chars
	version = strings.TrimRight(version, ".")

	// e.g. now version = "v0.9.12"
	if version == "" {
		return "", fmt.Errorf("Something went wrong. If you see the error 'tls: failed to verify certificate: x509: certificate signed by unknown authority' and you are NOT using a Dokcer scratch container, then you should try the following, assuming you are using linux: 'sudo apt update && sudo apt install --reinstall ca-certificates'")
	}

	return version, nil
}

// IsUsingNewestGophyVersion determines whether currently the newest available version of gophy is being run. Returns three parameters: bool (userIsUsingNewestVerision), string (newest available version string), error
func IsUsingNewestGophyVersion(version string) (bool, string, error) {
	url := "https://github.com/felix314159/gophy/releases/latest" // will forward to currently latest release

	// determine latest release of gophy
	latestVersion, err := getLatestGithubRelease(url)
	if err != nil {
		return false, "", err
	}

	// compare with currently used gophy version
	if latestVersion == version {
		return true, latestVersion, nil
	} 

	return false, latestVersion, nil
}
