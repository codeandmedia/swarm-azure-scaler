package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/martinlindhe/base36"
)

// Azure Sheduled Events nonroutable IP endpoint
const (
	url = "http://169.254.169.254/metadata/scheduledevents?api-version=2019-08-01"
)

type Data struct {
	Ent Ent `yaml:"services"`
}

type Ent interface{}

type Response struct {
	Documentincarnation int `json:"DocumentIncarnation"`
	Events              []struct {
		EventID      string   `json:"EventId,omitempty"`
		EventType    string   `json:"EventType,omitempty"`
		ResourceType string   `json:"ResourceType,omitempty"`
		Resources    []string `json:"Resources,omitempty"`
		EventStatus  string   `json:"EventStatus,omitempty"`
		NotBefore    string   `json:"NotBefore,omitempty"`
		Description  string   `json:"Description,omitempty"`
		EventSource  string   `json:"EventSource,omitempty"`
	}
}

type scheduledEventsPost struct {
	StartRequests []startRequest `json:"StartRequests,omitempty"`
}

type startRequest struct {
	EventID string `json:"EventId,omitempty"`
}

var ctx = context.Background()

var cli, _ = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())

func followEvents() {

	filter := filters.NewArgs()
	filter.Add("type", "node")
	filter.Add("event", "create")

	msg, errChan := cli.Events(ctx, types.EventsOptions{
		Filters: filter,
	})

	for {
		select {
		case err := <-errChan:
			log.Fatalln("Error getting Docker Swarm events", err)
		case <-msg:
			time.Sleep(10 * time.Second) // the gap between nodes event and its readiness
			go reCountServices()
		}
	}
}

func howMuchNodes() (counter int) {

	options, err := cli.NodeList(ctx, types.NodeListOptions{})
	if err != nil {
		log.Fatalln("Error getting Docker Swarm nodes", err)
	}

	for _, option := range options {

		if option.Status.State == "ready" && option.Spec.Availability == "active" {
			counter++
		}
	}

	return counter

}

func reCountServices() {

	dat, err := ioutil.ReadFile("/home/config.yaml")
	if err != nil {
		log.Println("YAML config file not found", err)
	}

	var out Data
	if err := yaml.Unmarshal([]byte(dat), &out); err != nil {
		log.Println("YAML config invalid, please check the docs", err)
	}

	i := out.Ent.(map[string]interface{})

	for k, v := range i {

		service, _, err := cli.ServiceInspectWithRaw(ctx, k, types.ServiceInspectOptions{})
		if err != nil {
			log.Fatalln("Service from config file doesn't exist", err)
		}

		multiplicator := strconv.Itoa(v.(int) * howMuchNodes())

		uintScale, err := strconv.ParseUint(multiplicator, 10, 64)

		service.Spec.Mode.Replicated.Replicas = &uintScale

		_, err = cli.ServiceUpdate(ctx, service.ID, service.Version, service.Spec, types.ServiceUpdateOptions{})

		log.Println("Service", k, "got replicated to", multiplicator)
	}

}

func workerAPI() {

	client := http.Client{}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Fatalln("Error with GET request to API", err)
	}
	req.Header.Add("Metadata", "true")
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalln("Azure Metadata Service not responding", err)
	}

	defer resp.Body.Close()

	bodyBytes, _ := ioutil.ReadAll(resp.Body)

	var responseObject Response
	json.Unmarshal(bodyBytes, &responseObject)

	for _, value := range responseObject.Events {

		if value.EventType == "Terminate" { // change it to "Preempt" if you're using Spot VMs

			for _, nodeName := range value.Resources {
				go nodeDrain(nodeName)
			}

			evid := value.EventID
			go postToAPI(evid)
		}
	}
}

func postToAPI(evid string) {

	time.Sleep(45 * time.Second)

	go reCountServices()

	body, _ := json.Marshal(scheduledEventsPost{[]startRequest{{EventID: evid}}})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		log.Println("Error with POST request to API", err)
	}

	req.Header.Add("Metadata", "true")

	client := &http.Client{}

	res, err := client.Do(req)
	if err != nil {
		log.Println("Azure Metadata Service not responding", err)
	}
	_, err = ioutil.ReadAll(res.Body)
	defer res.Body.Close()

}

func convertToHostname(id string) string {

	number := strings.Split(id, "_")[1]
	name := strings.Split(id, "_")[0]

	toUint, _ := strconv.ParseUint(number, 10, 64)

	switch {
	case toUint > 1676015:
		return name + "0" + base36.Encode(toUint)
	case toUint > 46655:
		return name + "00" + base36.Encode(toUint)
	case toUint > 1295:
		return name + "000" + base36.Encode(toUint)
	case toUint > 35:
		return name + "0000" + base36.Encode(toUint)
	default:
		return name + "00000" + base36.Encode(toUint)
	}
}

func nodeDrain(nodeName string) {

	nodeHostName := convertToHostname(nodeName)

	node, _, err := cli.NodeInspectWithRaw(ctx, nodeHostName)
	if err != nil {
		log.Fatalln("Docker Swarm API not responding", err)
	}

	spec := node.Spec
	spec.Availability = "drain"
	err = cli.NodeUpdate(ctx, node.ID, node.Version, spec)

	if err != nil {
		log.Fatalln("Docker Swarm API not responding", err)
	}

	time.Sleep(1 * time.Minute) // take more time if your node need more to graceful shutdown, but also do not forget to increase POST delay

	err = cli.NodeRemove(ctx, node.ID, types.NodeRemoveOptions{Force: true})

	if err != nil {
		log.Fatalln("Docker Swarm API not responding", err)
	}

	log.Println("Node", nodeName, "got drained and removed")
}

func tickerAPI() {
	c := time.Tick(2 * time.Minute) // NotBefore property setup on 5min
	for range c {
		workerAPI()
	}
}

func main() {

	go tickerAPI()
	followEvents()

}
