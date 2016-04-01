package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"

	"bytes"
	"github.com/klauspost/compress/zip"
	"gopkg.in/ini.v1"
	"os"
	"path/filepath"
	"strings"
)

type Configuration struct {
	Username        string
	Password        string
	ApiUrl          string
	OutputDirectory string
}

type Library struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

const (
	configurationFile = "client.ini"
	pathPing          = "/ping/"
	pathAuthToken     = "/auth-token/"
	pathAuthPing      = "/auth/ping/"
	pathLibraries     = "/repos/"
	pathDir           = "/dir/"
)

var (
	client = http.DefaultClient
)

func loadConfig(configName string) (*Configuration, error) {
	cfg, err := ini.Load(configName)
	if err != nil {
		return nil, err
	}

	general, err := cfg.GetSection("general")
	if err != nil {
		return nil, err
	}

	username, err := general.GetKey("username")
	if err != nil {
		return nil, err
	}

	password, err := general.GetKey("password")
	if err != nil {
		return nil, err
	}

	url, err := general.GetKey("url")
	if err != nil {
		return nil, err
	}

	var outputString string
	output, err := general.GetKey("output")
	if err != nil {
		outputString = "data"
	} else {
		outputString = output.String()
	}

	return &Configuration{
		Username:        username.String(),
		Password:        password.String(),
		ApiUrl:          url.String(),
		OutputDirectory: outputString,
	}, nil
}

func pingTest(c *Configuration) error {
	resp, err := client.Get(c.ApiUrl + pathPing)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected response code %d, but received %d", http.StatusOK, resp.StatusCode)
	}

	return nil
}

func getToken(c *Configuration) (string, error) {
	data := url.Values{}
	data.Add("username", c.Username)
	data.Add("password", c.Password)
	resp, err := client.PostForm(c.ApiUrl+pathAuthToken, data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	type AuthToken struct {
		Token string `json:"token"`
	}

	binaryBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var authToken AuthToken
	err = json.Unmarshal(binaryBody, &authToken)
	if err != nil {
		return "", err
	}

	return authToken.Token, nil
}

func authPingTest(c *Configuration, token string) error {
	req, err := http.NewRequest("GET", c.ApiUrl+pathAuthPing, nil)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Token "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected response code %d, but received %d", http.StatusOK, resp.StatusCode)
	}

	return nil
}

func listLibraries(c *Configuration, token string) ([]Library, error) {
	req, err := http.NewRequest("GET", c.ApiUrl+pathLibraries, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Token "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	bodyBinary, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var libraries []Library
	err = json.Unmarshal(bodyBinary, &libraries)
	if err != nil {
		return nil, err
	}

	return libraries, nil
}

func requestDownloadLink(c *Configuration, token string, id string) (string, error) {
	req, err := http.NewRequest("GET", c.ApiUrl+pathLibraries+id+pathDir+"download/?p=/", nil)
	if err != nil {
		return "", err
	}

	req.Header.Add("Authorization", "Token "+token)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	bodyBinary, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("expected status code %d, but received %d", http.StatusOK, resp.StatusCode)
	}

	return strings.Trim(string(bodyBinary), "\""), nil
}

func downloadLibrary(c *Configuration, library Library, downloadLink string) error {
	resp, err := client.Get(downloadLink)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Expected status code %d, but received %d", http.StatusOK, resp.StatusCode)
	}

	file, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	reader := bytes.NewReader(file)
	zipReader, err := zip.NewReader(reader, int64(len(file)))
	if err != nil {
		return err
	}

	for _, file := range zipReader.File {
		rc, err := file.Open()
		if err != nil {
			log.Println("Unable to open file within zip:", file.Name, err)
			continue
		}

		dir := filepath.Dir(file.Name)
		if len(dir) > 0 {
			err = os.MkdirAll(filepath.Join(c.OutputDirectory, dir), os.FileMode(0755))
			if err != nil {
				log.Println("Unable to create output directory", filepath.Join(dir, dir), "within zip:", err)
				continue
			}
		}

		data, err := ioutil.ReadAll(rc)
		if err != nil {
			log.Println("Unable to read file within zip:", file.Name, err)
			continue
		}

		err = ioutil.WriteFile(filepath.Join(c.OutputDirectory, file.Name), data, os.FileMode(0755))
		if err != nil {
			log.Println("Unable to write output file from zip:", filepath.Join(c.OutputDirectory, file.Name), err)
		}
	}
	return nil
}

func main() {
	config, err := loadConfig(configurationFile)
	if err != nil {
		log.Fatalln("Unable to parse configuration file:", err)
	}

	err = os.MkdirAll(config.OutputDirectory, os.FileMode(0755))
	if err != nil {
		log.Fatalln("Unable to create output directory", config.OutputDirectory, ":", err)
	}

	err = pingTest(config)
	if err != nil {
		log.Fatalln("Unable to ping:", err)
	}

	token, err := getToken(config)
	if err != nil {
		log.Fatalln("Unable to get auth token:", err)
	}

	err = authPingTest(config, token)
	if err != nil {
		log.Fatalln("Unable to auth ping:", err)
	}

	libraries, err := listLibraries(config, token)
	if err != nil {
		log.Fatalln("Unable to list libraries:", err)
	}

	for _, library := range libraries {
		dlLink, err := requestDownloadLink(config, token, library.Id)
		if err != nil {
			log.Println("Unable to request download link for library", library.Name, err)
		}

		err = downloadLibrary(config, library, dlLink)
		if err != nil {
			log.Println("Unable to download library:", library.Name, err)
		}
	}
	fmt.Println("Libraries:", libraries)
}
