package config

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

var (
	GlobalConfig *Config
)

type Config struct {
	Port     int    `json:"port"`
	LogLevel string `json:"logLevel"`

	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
	Zone          string `json:"zone"`
	Camera        string `json:"camera"`
	FoudaryAddr   string `json:"foudary_addr"`
	WebsocketAddr string `json:"websocket_addr"`
	RtspAddr      string `json:"rtsp_addr"`
	UseOpenCV     bool   `json:"use_opencv"`

	Token  string
	TaskID string
}

func init() {
	LoadConfig()
}

func LoadConfig() *Config {
	// 读取配置文件内容
	file, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal("Failed to read config file:", err)
		return nil
	}

	// 解析配置文件内容
	var config Config
	err = json.Unmarshal(file, &config)
	if err != nil {
		log.Fatal("Failed to parse config file:", err)
		return nil
	}

	GlobalConfig = &config

	err = RefreshToken(&config)
	if err != nil {
		log.Fatal("Failed RefreshToken:", err)
		return nil
	}
	err = GetRTSPInfo(&config)
	if err != nil {
		log.Fatal("Failed GetRTSPInfo:", err)
		return nil
	}

	log.Infof("RtspAddr: %v", config.RtspAddr)
	log.Infof("WebsocketAddr: %v", config.WebsocketAddr)
	log.Infof("UseOpenCV: %v", config.UseOpenCV)
	return &config
}

type RequestBody struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

type ResponseBody struct {
	Token string `json:"token"`
}

func RefreshToken(config *Config) error {
	token, err := GetToken()
	if err != nil {
		log.Errorf("get token failed, %v", err)
		return err
	}
	config.Token = token
	config.WebsocketAddr = fmt.Sprintf("%s?jwt=%s", config.WebsocketAddr, token)
	return nil
}

func GetToken() (token string, err error) {
	url := fmt.Sprintf("https://%s/components/user_manager/v1/users/sign_token", GlobalConfig.FoudaryAddr)

	requestBody := RequestBody{
		AccessKey: GlobalConfig.AccessKey,
		SecretKey: GlobalConfig.SecretKey,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		log.Debugln("Failed to marshal JSON:", err)
		return
	}

	request, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Debugln("Failed to create request:", err)
		return
	}

	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: time.Second * 10, // 设置请求超时时间
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	response, err := client.Do(request)
	if err != nil {
		log.Debugln("RefreshToken Failed to send request:", err)
		return
	}
	defer response.Body.Close()

	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		log.Debugln("RefreshToken Failed to read response body:", err)
		return
	}

	if response.StatusCode != http.StatusOK {
		log.Debugln("RefreshToken Request failed with status code:", response.StatusCode)
		log.Debugln("RefreshToken Response body:", string(responseData))
		return
	}

	var responseBody ResponseBody
	err = json.Unmarshal(responseData, &responseBody)
	if err != nil {
		log.Debugln("RefreshToken Failed to unmarshal response:", err)
		return
	}

	log.Infof("Token:%s", responseBody.Token)
	return responseBody.Token, nil
}

type Task struct {
	ID             string `json:"id"`
	ZoneUUID       string `json:"zone_uuid"`
	CameraUUID     string `json:"camera_uuid"`
	ObjectType     string `json:"object_type"`
	FeatureVersion int    `json:"feature_version"`
	// TaskObjectConfig Config      `json:"task_object_config"`
	PlaybackConfig interface{} `json:"playback_config"`
	IngressType    string      `json:"ingress_type"`
	// TaskStatus         Status      `json:"task_status"`
	InternalTaskUUID string      `json:"internal_task_uuid"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
	StoragePolicy    interface{} `json:"storage_policy"`
	// VideoParameter     VideoParam  `json:"video_parameter"`
	// ExtraParameter     ExtraParam  `json:"extra_parameter"`
	SymphonyDeviceTask interface{} `json:"symphony_device_task"`
	UUID               string      `json:"uuid"`
	DataMining         bool        `json:"data_mining"`
	UserData           string      `json:"user_data"`
}

type Response struct {
	Tasks []Task `json:"tasks"`
	Page  struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
		Total  int `json:"total"`
	} `json:"page"`
}

type TaskResponse struct {
	// Info               TaskInfo   `json:"info"`
	// Status             TaskStatus `json:"status"`
	RtspPreviewAddress string `json:"rtsp_preview_address"`
}

func GetRTSPInfo(config *Config) error {
	url := fmt.Sprintf("https://%s/engine/camera-manager/v1/zones/%s/cameras/%s/tasks?page.offset=0&page.limit=100",
		config.FoudaryAddr, config.Zone, config.Camera)
	log.Debugf("url: %v", url)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Debugln("GetRTSPInfo Failed to create request:", err)
		return err
	}

	jwt := fmt.Sprintf("Bearer %s", config.Token)
	request.Header.Set("Authorization", jwt)

	client := &http.Client{
		Timeout: time.Second * 10, // 设置请求超时时间
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	response, err := client.Do(request)
	if err != nil {
		log.Debugln("GetRTSPInfo Failed to send request:", err)
		return err
	}
	defer response.Body.Close()

	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		log.Debugln("GetRTSPInfo Failed to read response body:", err)
		return err
	}

	if response.StatusCode != http.StatusOK {
		log.Debugln("GetRTSPInfo Request failed with status code:", response.StatusCode)
		log.Debugln("GetRTSPInfo Response body:", string(responseData))
		return err
	}

	var resp Response
	err = json.Unmarshal(responseData, &resp)
	if err != nil {
		log.Debugln("GetRTSPInfo Failed to unmarshal response:", err)
		return err
	}
	log.Debugf("GetRTSPInfo Response body: %v", resp)
	config.TaskID = resp.Tasks[0].InternalTaskUUID

	url = fmt.Sprintf("https://%s/engine/video-process/v1/tasks/%s",
		config.FoudaryAddr, config.TaskID)
	log.Debugf("url: %v", url)
	request, err = http.NewRequest("GET", url, nil)
	if err != nil {
		log.Debugln("Failed to create request:", err)
		return err
	}

	request.Header.Set("Authorization", jwt)

	response, err = client.Do(request)
	if err != nil {
		log.Debugln("Failed to send request:", err)
		return err
	}
	defer response.Body.Close()

	responseData, err = io.ReadAll(response.Body)
	if err != nil {
		log.Debugln("Failed to read response body:", err)
		return err
	}
	log.Debugln("Response body:", string(responseData))
	if response.StatusCode != http.StatusOK {
		log.Debugln("Request failed with status code:", response.StatusCode)
		log.Debugln("Response body:", string(responseData))
		return err
	}

	var taskResp TaskResponse
	err = json.Unmarshal(responseData, &taskResp)
	if err != nil {
		log.Debugln("Failed to unmarshal response:", err)
		return err
	}
	// config.RtspAddr = strings.TrimRight(config.RtspAddr, "/")
	// config.RtspAddr = fmt.Sprintf("%s/%s", config.RtspAddr, config.TaskID)
	config.RtspAddr = taskResp.RtspPreviewAddress
	return nil
}
