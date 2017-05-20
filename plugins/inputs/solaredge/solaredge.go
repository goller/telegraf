package solaredge

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

const serverURL = "https://monitoringapi.solaredge.com/equipment"

// SolarEdge struct
type SolarEdge struct {
	Name         string
	SiteID       string `toml:"site_id"`
	SerialNumber string
	APIKey       string `toml:"api_key"`
	TimeZone     string

	client          HTTPClient
	ResponseTimeout internal.Duration
}

type Equipment struct {
	Data struct {
		Count       int `json:"count"`
		Telemetries []struct {
			Date                  string  `json:"date"`
			TotalActivePower      float64 `json:"totalActivePower"`
			DcVoltage             float64 `json:"dcVoltage"`
			GroundFaultResistance float64 `json:"groundFaultResistance"`
			PowerLimit            float64 `json:"powerLimit"`
			TotalEnergy           float64 `json:"totalEnergy"`
			Temperature           float64 `json:"temperature"`
			InverterMode          string  `json:"inverterMode"`
			L1Data                struct {
				AcCurrent     float64 `json:"acCurrent"`
				AcVoltage     float64 `json:"acVoltage"`
				AcFrequency   float64 `json:"acFrequency"`
				ApparentPower float64 `json:"apparentPower"`
				ActivePower   float64 `json:"activePower"`
				ReactivePower float64 `json:"reactivePower"`
				CosPhi        float64 `json:"cosPhi"`
			} `json:"L1Data"`
		} `json:"telemetries"`
	} `json:"data"`
}

type HTTPClient interface {
	MakeRequest(req *http.Request) (*http.Response, error)

	SetHTTPClient(client *http.Client)
	HTTPClient() *http.Client
}

type RealHTTPClient struct {
	client *http.Client
}

func (c *RealHTTPClient) MakeRequest(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

func (c *RealHTTPClient) SetHTTPClient(client *http.Client) {
	c.client = client
}

func (c *RealHTTPClient) HTTPClient() *http.Client {
	return c.client
}

var sampleConfig = `
  ## a name for the service being polled
  name = "solaredge"

  ## Set response_timeout (default 5 seconds)
  response_timeout = "5s"

  # Your specific site id
  # site_id = "123456"

  # Your serial number for your inverter
  # serial_number = "12345678-00"

  # Your SolarEdge API Key
  # api_key = "L4QLVQ1LOKCQX2193VSEICXW61NP6B1O"
  # time_zone = "MST"
`

func (s *SolarEdge) SampleConfig() string {
	return sampleConfig
}

func (s *SolarEdge) Description() string {
	return "Read SolarEdge Inverter data"
}

// Gathers data from SolarEdge.
func (s *SolarEdge) Gather(acc telegraf.Accumulator) error {
	if s.client.HTTPClient() == nil {
		tr := &http.Transport{
			ResponseHeaderTimeout: s.ResponseTimeout.Duration,
		}
		client := &http.Client{
			Transport: tr,
			Timeout:   s.ResponseTimeout.Duration,
		}
		s.client.SetHTTPClient(client)
	}

	equipmentURL := fmt.Sprintf("%s/%s/%s/data", serverURL, s.SiteID, s.SerialNumber)
	requestURL, err := url.Parse(equipmentURL)
	if err != nil {
		return fmt.Errorf("Invalid server URL \"%s\"", equipmentURL)
	}

	loc, _ := time.LoadLocation(s.TimeZone)
	endTime := time.Now().In(loc)
	startTime := endTime.Add(-time.Duration(6 * 24 * time.Hour))

	params := requestURL.Query()
	params.Add("api_key", s.APIKey)

	params.Add("startTime", startTime.Format("2006-01-02 15:04:05"))
	params.Add("endTime", endTime.Format("2006-01-02 15:04:05"))
	requestURL.RawQuery = params.Encode()

	log.Printf("URL FOR SOLAREDGE: %s", requestURL)
	start := time.Now()
	resp, err := http.Get(requestURL.String())
	if err != nil {
		log.Printf("Error %v", err)
		return err
	}
	responseTime := time.Since(start).Seconds()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Error %v", err)
		err = fmt.Errorf("Response from url \"%s\" has status code %d (%s), expected %d (%s)",
			requestURL.String(),
			resp.StatusCode,
			http.StatusText(resp.StatusCode),
			http.StatusOK,
			http.StatusText(http.StatusOK))
		return err
	}

	var target Equipment
	if err := json.NewDecoder(resp.Body).Decode(&target); err != nil {
		log.Printf("Error %v", err)
		return err
	}

	tags := map[string]string{}
	for _, telemetry := range target.Data.Telemetries {
		date, err := time.ParseInLocation("2006-01-02 15:04:05", telemetry.Date, loc)
		if err != nil {
			log.Printf("Error %v", err)
			return err
		}
		fields := make(map[string]interface{})
		fields["response_time"] = responseTime
		fields["totalActivePower"] = telemetry.TotalActivePower
		fields["dcVoltage"] = telemetry.DcVoltage
		fields["groundFaultResistance"] = telemetry.GroundFaultResistance
		fields["powerLimit"] = telemetry.PowerLimit
		fields["totalEnergy"] = telemetry.TotalEnergy
		fields["temperature"] = telemetry.Temperature
		fields["inverterMode"] = telemetry.InverterMode
		fields["acCurrent"] = telemetry.L1Data.AcCurrent
		fields["acVoltage"] = telemetry.L1Data.AcVoltage
		fields["acFrequency"] = telemetry.L1Data.AcFrequency
		fields["apparentPower"] = telemetry.L1Data.ApparentPower
		fields["activePower"] = telemetry.L1Data.ActivePower
		fields["reactivePower"] = telemetry.L1Data.ReactivePower
		fields["cosPhi"] = telemetry.L1Data.CosPhi
		log.Printf("FIELDS %v", fields)
		acc.AddFields(s.Name, fields, tags, date)
	}
	return nil
}

func init() {
	inputs.Add("solaredge", func() telegraf.Input {
		return &SolarEdge{
			client: &RealHTTPClient{},
			ResponseTimeout: internal.Duration{
				Duration: 5 * time.Second,
			},
		}
	})
}
