package enpal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vmihailenco/msgpack/v5"
)

// CollectorData represents the structure of the collector response
type CollectorData struct {
	CollectionID      string               `json:"collectionId"`
	IoTDeviceID       string               `json:"ioTDeviceId"`
	CollectionType    string               `json:"collectionType"`
	TimeStampUtc      string               `json:"timeStampUtc"`
	ErrorCodes        []ErrorCode          `json:"errorCodes"`
	DeviceCollections []DeviceCollection   `json:"DeviceCollections"`
	EnergyManagement  []EnergyManagement   `json:"EnergyManagement"`
	MetaData          interface{}          `json:"metaData"`
	TroubleCodes      []interface{}        `json:"troubleCodes"`
	NumberDataPoints  map[string]DataPoint `json:"numberDataPoints"`
	TextDataPoints    map[string]DataPoint `json:"textDataPoints"`
}

type ErrorCode struct {
	TimeStampUtc string `json:"timeStampUtc"`
	ErrorCode    string `json:"errorCode"`
}

type DeviceCollection struct {
	DeviceID         string               `json:"deviceId"`
	IngestionKey     string               `json:"ingestionKey"`
	DeviceClass      string               `json:"deviceClass"`
	TimeStampUtc     string               `json:"timeStampUtc"`
	ErrorCodes       []ErrorCode          `json:"errorCodes"`
	NumberDataPoints map[string]DataPoint `json:"numberDataPoints"`
	TextDataPoints   map[string]DataPoint `json:"textDataPoints"`
}

type DataPoint struct {
	TimeStampUtcOfMeasurement string `json:"timeStampUtcOfMeasurement"`
	Unit                      string `json:"unit"`
	Value                     any    `json:"value,omitempty"`
}

type EnergyManagement struct {
	EnergyManagementID string               `json:"energyManagementId"`
	ReferenceDeviceID  string               `json:"referenceDeviceId"`
	TimeStampUtc       string               `json:"timeStampUtc"`
	NumberDataPoints   map[string]DataPoint `json:"numberDataPoints"`
	TextDataPoints     map[string]DataPoint `json:"textDataPoints"`
	ErrorCodes         []ErrorCode          `json:"errorCodes"`
}

// FetchCollectorData connects to the Enpal device and fetches the current collector state.
// baseURL should be like "http://192.168.1.123"
// Returns the raw JSON string and parsed data, or an error.
func FetchCollectorData(ctx context.Context, baseURL string) (string, *CollectorData, error) {
	client := &enpalClient{baseURL: baseURL}
	return client.fetch(ctx)
}

// enpalClient handles the Blazor WebSocket connection
type enpalClient struct {
	baseURL          string
	conn             *websocket.Conn
	httpClient       *http.Client
	components       []componentDescriptor
	applicationState string
	resultChan       chan string
	errorChan        chan error
}

type componentDescriptor struct {
	Type        string       `json:"type"`
	Sequence    int          `json:"sequence"`
	Descriptor  string       `json:"descriptor"`
	PrerenderID string       `json:"prerenderId"`
	Key         componentKey `json:"key"`
}

type componentKey struct {
	LocationHash          string `json:"locationHash"`
	FormattedComponentKey string `json:"formattedComponentKey"`
}

func (c *enpalClient) fetch(ctx context.Context) (string, *CollectorData, error) {
	c.resultChan = make(chan string, 1)
	c.errorChan = make(chan error, 1)

	// Create HTTP client with cookie jar
	jar, _ := cookiejar.New(nil)
	c.httpClient = &http.Client{Jar: jar, Timeout: 30 * time.Second}

	// Step 1: Visit collector page to get session + components
	collectorResp, err := c.httpClient.Get(c.baseURL + "/collector")
	if err != nil {
		return "", nil, fmt.Errorf("failed to visit collector: %w", err)
	}
	htmlBytes, err := io.ReadAll(collectorResp.Body)
	collectorResp.Body.Close()
	if err != nil {
		return "", nil, fmt.Errorf("failed to read HTML: %w", err)
	}

	c.components = extractComponents(string(htmlBytes))
	c.applicationState = extractAppState(string(htmlBytes))

	if len(c.components) == 0 || c.applicationState == "" {
		return "", nil, fmt.Errorf("failed to extract Blazor components from HTML")
	}

	// Step 2: Negotiate
	resp, err := c.httpClient.Post(c.baseURL+"/_blazor/negotiate?negotiateVersion=1", "text/plain", nil)
	if err != nil {
		return "", nil, fmt.Errorf("negotiate failed: %w", err)
	}
	defer resp.Body.Close()

	var negResp struct {
		ConnectionToken string `json:"connectionToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&negResp); err != nil {
		return "", nil, fmt.Errorf("decode negotiate failed: %w", err)
	}

	// Step 3: Connect WebSocket
	host := strings.TrimPrefix(c.baseURL, "http://")
	wsURL := fmt.Sprintf("ws://%s/_blazor?id=%s", host, negResp.ConnectionToken)

	header := http.Header{}
	if cookies := c.httpClient.Jar.Cookies(resp.Request.URL); len(cookies) > 0 {
		var cookieStrs []string
		for _, cookie := range cookies {
			cookieStrs = append(cookieStrs, cookie.String())
		}
		header.Add("Cookie", strings.Join(cookieStrs, "; "))
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return "", nil, fmt.Errorf("websocket dial failed: %w", err)
	}
	c.conn = conn
	defer c.conn.Close()

	// Step 4: Handshake
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"protocol":"blazorpack","version":1}`+"\x1e")); err != nil {
		return "", nil, fmt.Errorf("handshake failed: %w", err)
	}

	_, hsResp, err := conn.ReadMessage()
	if err != nil {
		return "", nil, fmt.Errorf("read handshake failed: %w", err)
	}

	var hs map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSuffix(hsResp, []byte{0x1e}), &hs); err != nil {
		return "", nil, fmt.Errorf("parse handshake failed: %w", err)
	}
	if errMsg, ok := hs["error"].(string); ok && errMsg != "" {
		return "", nil, fmt.Errorf("handshake error: %s", errMsg)
	}

	// Start message reader
	go c.readLoop(ctx)

	// Step 5: StartCircuit
	if err := c.sendStartCircuit(); err != nil {
		return "", nil, fmt.Errorf("StartCircuit failed: %w", err)
	}

	time.Sleep(300 * time.Millisecond)

	// Step 6: UpdateRootComponents
	if err := c.sendUpdateRootComponents(); err != nil {
		return "", nil, fmt.Errorf("UpdateRootComponents failed: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Step 7: Click button
	if err := c.clickButton(4); err != nil {
		return "", nil, fmt.Errorf("click button failed: %w", err)
	}

	// Wait for result with timeout
	select {
	case result := <-c.resultChan:
		var data CollectorData
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			return result, nil, nil // Return raw JSON if unmarshal fails
		}
		return result, &data, nil
	case err := <-c.errorChan:
		return "", nil, err
	case <-time.After(15 * time.Second):
		return "", nil, fmt.Errorf("timeout waiting for data")
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}
}

func (c *enpalClient) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.errorChan <- fmt.Errorf("read error: %w", err)
			return
		}

		c.handleMessage(data)
	}
}

func (c *enpalClient) handleMessage(data []byte) {
	reader := bytes.NewReader(data)

	for reader.Len() > 0 {
		length, err := readVLQ(reader)
		if err != nil {
			return
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return
		}

		var msg []interface{}
		if err := msgpack.Unmarshal(payload, &msg); err != nil {
			continue
		}

		if len(msg) < 4 {
			continue
		}

		msgType := toInt64(msg[0])

		if msgType == 1 { // Invocation
			target, _ := msg[3].(string)
			args, _ := msg[4].([]interface{})

			switch target {
			case "JS.RenderBatch":
				if len(args) >= 1 {
					batchID := toInt(args[0])
					c.sendOnRenderCompleted(batchID)
				}
			case "JS.BeginInvokeJS":
				if len(args) >= 3 {
					taskID := toInt64(args[0])
					identifier, _ := args[1].(string)

					// Check for our data
					if identifier == "blazorMonaco.editor.setValue" {
						if dataStr, ok := args[2].(string); ok {
							jsonData := extractJSON(dataStr)
							if jsonData != "" {
								c.resultChan <- jsonData
							}
						}
					}

					// Send response
					c.sendEndInvokeJS(taskID)
				}
			case "JS.Error":
				if len(args) > 0 {
					c.errorChan <- fmt.Errorf("server error: %v", args[0])
				}
			}
		}
	}
}

func extractJSON(rawData string) string {
	var dataArray []string
	if err := json.Unmarshal([]byte(rawData), &dataArray); err != nil {
		return ""
	}
	if len(dataArray) >= 2 {
		return dataArray[1]
	}
	return ""
}

func (c *enpalClient) sendMessage(msgArray []interface{}) error {
	payload, err := msgpack.Marshal(msgArray)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	writeVLQ(&buf, len(payload))
	buf.Write(payload)

	return c.conn.WriteMessage(websocket.BinaryMessage, buf.Bytes())
}

func (c *enpalClient) sendStartCircuit() error {
	msg := []interface{}{
		int64(1),
		map[string]interface{}{},
		"0",
		"StartCircuit",
		[]interface{}{c.baseURL + "/", c.baseURL + "/collector", "[]", c.applicationState},
	}
	return c.sendMessage(msg)
}

func (c *enpalClient) sendUpdateRootComponents() error {
	var operations []map[string]interface{}
	for i, comp := range c.components {
		operations = append(operations, map[string]interface{}{
			"type":           "add",
			"ssrComponentId": i + 1,
			"marker": map[string]interface{}{
				"type":        comp.Type,
				"prerenderId": comp.PrerenderID,
				"key": map[string]interface{}{
					"locationHash":          comp.Key.LocationHash,
					"formattedComponentKey": comp.Key.FormattedComponentKey,
				},
				"sequence":   comp.Sequence,
				"descriptor": comp.Descriptor,
				"uniqueId":   i,
			},
		})
	}

	batch := map[string]interface{}{
		"batchId":    1,
		"operations": operations,
	}
	batchJSON, _ := json.Marshal(batch)

	msg := []interface{}{
		int64(1),
		map[string]interface{}{},
		nil,
		"UpdateRootComponents",
		[]interface{}{string(batchJSON), c.applicationState},
	}
	return c.sendMessage(msg)
}

func (c *enpalClient) clickButton(eventHandlerID int) error {
	eventInfo := map[string]interface{}{
		"eventHandlerId": eventHandlerID,
		"eventName":      "click",
		"eventFieldInfo": nil,
	}
	eventArgs := map[string]interface{}{
		"detail": 1, "button": 0, "buttons": 0,
		"ctrlKey": false, "shiftKey": false, "altKey": false, "metaKey": false,
		"type": "click",
	}
	eventJSON, _ := json.Marshal([]interface{}{eventInfo, eventArgs})

	msg := []interface{}{
		int64(1),
		map[string]interface{}{},
		nil,
		"BeginInvokeDotNetFromJS",
		[]interface{}{"1", nil, "DispatchEventAsync", int8(1), string(eventJSON)},
	}
	return c.sendMessage(msg)
}

func (c *enpalClient) sendOnRenderCompleted(batchID int) error {
	msg := []interface{}{
		int64(1),
		map[string]interface{}{},
		nil,
		"OnRenderCompleted",
		[]interface{}{int64(batchID), nil},
	}
	return c.sendMessage(msg)
}

func (c *enpalClient) sendEndInvokeJS(taskID int64) error {
	resultJSON := fmt.Sprintf("[%d,true,null]", taskID)
	msg := []interface{}{
		int64(1),
		map[string]interface{}{},
		nil,
		"EndInvokeJSFromDotNet",
		[]interface{}{taskID, true, resultJSON},
	}
	return c.sendMessage(msg)
}

// Helper functions

func extractComponents(html string) []componentDescriptor {
	pattern := regexp.MustCompile(`<!--Blazor:(\{.+?\})-->`)
	matches := pattern.FindAllStringSubmatch(html, -1)

	var components []componentDescriptor
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		jsonStr := strings.ReplaceAll(match[1], `\u002B`, "+")
		jsonStr = strings.ReplaceAll(jsonStr, `\u002F`, "/")

		var comp map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &comp); err != nil {
			continue
		}

		if compType, ok := comp["type"].(string); ok && compType == "server" {
			if descriptor, ok := comp["descriptor"].(string); ok && descriptor != "" {
				c := componentDescriptor{Type: compType, Descriptor: descriptor}
				if seq, ok := comp["sequence"].(float64); ok {
					c.Sequence = int(seq)
				}
				if preID, ok := comp["prerenderId"].(string); ok {
					c.PrerenderID = preID
				}
				if keyObj, ok := comp["key"].(map[string]interface{}); ok {
					if lh, ok := keyObj["locationHash"].(string); ok {
						c.Key.LocationHash = lh
					}
					if fk, ok := keyObj["formattedComponentKey"].(string); ok {
						c.Key.FormattedComponentKey = fk
					}
				}
				components = append(components, c)
			}
		}
	}
	return components
}

func extractAppState(html string) string {
	pattern := regexp.MustCompile(`<!--Blazor-Server-Component-State:([^-]+)-->`)
	matches := pattern.FindStringSubmatch(html)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func writeVLQ(w io.Writer, value int) {
	for {
		b := byte(value & 0x7F)
		value >>= 7
		if value > 0 {
			b |= 0x80
		}
		w.Write([]byte{b})
		if value == 0 {
			break
		}
	}
}

func readVLQ(r io.Reader) (int, error) {
	result, shift := 0, 0
	for {
		var b [1]byte
		if _, err := r.Read(b[:]); err != nil {
			return 0, err
		}
		result |= int(b[0]&0x7F) << shift
		if b[0]&0x80 == 0 {
			break
		}
		shift += 7
	}
	return result, nil
}

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int8:
		return int64(n)
	case int16:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint8:
		return int64(n)
	case uint16:
		return int64(n)
	case uint32:
		return int64(n)
	case uint64:
		return int64(n)
	}
	return 0
}

func toInt(v interface{}) int {
	return int(toInt64(v))
}
