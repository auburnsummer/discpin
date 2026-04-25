package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var netClient = &http.Client{
	Timeout: 10 * time.Second,
}

func isDiscordWebhookURL(url string) bool {
	return len(url) > 0 && strings.HasPrefix(url, "https://discord.com/api/webhooks/")
}

type Message struct {
	Id        string `json:"id"`
	ChannelId string `json:"channel_id"`
	WebhookId string `json:"webhook_id"`
}

type MessageResponse struct {
	Message Message `json:"message"`
}

type PinsResponse struct {
	Items []MessageResponse `json:"items"`
}

func exec(w http.ResponseWriter, r *http.Request) {
	var url = r.URL.Query().Get("url")
	var token = r.URL.Query().Get("token")

	if url == "" || token == "" {
		http.Error(w, "Missing url or token", http.StatusBadRequest)
		return
	}
	if !isDiscordWebhookURL(url) {
		http.Error(w, "Invalid Discord webhook URL", http.StatusBadRequest)
		return
	}

	// forward body to the webhook
	// add wait=true to the query parameters to wait for the response
	var forwardURL = url + "?wait=true"
	req, err := http.NewRequest("POST", forwardURL, r.Body)
	if err != nil {
		http.Error(w, "Failed to create request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	resp, err := netClient.Do(req)
	if err != nil {
		http.Error(w, "Failed to forward request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode > 299 || resp.StatusCode < 200 {
		fmt.Printf("Received status code %d from Discord webhook\n", resp.StatusCode)
		return
	}

	// decode the response body into JSON
	var responseData Message
	err = json.NewDecoder(resp.Body).Decode(&responseData)
	if err != nil {
		http.Error(w, "Failed to decode response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// pin the message /channels/{channel.id}/messages/pins/{message.id}
	var pinURL = fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages/pins/%s", responseData.ChannelId, responseData.Id)
	pinReq, err := http.NewRequest("PUT", pinURL, nil)
	if err != nil {
		http.Error(w, "Failed to create pin request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	pinReq.Header.Set("Authorization", "Bot "+token)
	pinResp, err := netClient.Do(pinReq)
	if err != nil {
		http.Error(w, "Failed to pin message: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer pinResp.Body.Close()

	// unpin previous message from same channel and author
	// get pinned messages /channels/{channel.id}/messages/pins
	var pinsURL = fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages/pins", responseData.ChannelId)
	pinsReq, err := http.NewRequest("GET", pinsURL, nil)
	if err != nil {
		http.Error(w, "Failed to create pins request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	pinsReq.Header.Set("Authorization", "Bot "+token)
	pinsResp, err := netClient.Do(pinsReq)
	if err != nil {
		http.Error(w, "Failed to get pinned messages: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer pinsResp.Body.Close()

	if pinsResp.StatusCode > 299 || pinsResp.StatusCode < 200 {
		fmt.Printf("Received status code %d from Discord pins request\n", pinsResp.StatusCode)
		return
	}

	var pinsData PinsResponse
	err = json.NewDecoder(pinsResp.Body).Decode(&pinsData)
	if err != nil {
		http.Error(w, "Failed to decode pins response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for _, item := range pinsData.Items {
		pin := item.Message
		if pin.Id == "" || pin.ChannelId == "" {
			continue
		}
		if pin.WebhookId != responseData.WebhookId {
			continue
		}
		if pin.Id == responseData.Id {
			continue
		}
		// unpin the message /channels/{channel.id}/messages/pins/{message.id}
		var unpinURL = fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages/pins/%s", responseData.ChannelId, pin.Id)
		unpinReq, err := http.NewRequest("DELETE", unpinURL, nil)
		if err != nil {
			http.Error(w, "Failed to create unpin request: "+err.Error(), http.StatusInternalServerError)
			return
		}
		unpinReq.Header.Set("Authorization", "Bot "+token)
		unpinResp, err := netClient.Do(unpinReq)
		if err != nil {
			http.Error(w, "Failed to unpin message: "+err.Error(), http.StatusInternalServerError)
			return
		}
		unpinResp.Body.Close()
	}

	fmt.Fprintf(w, "Message sent successfully! Message ID: %s\n", responseData.Id)
}

func main() {
	http.HandleFunc("/exec", exec)

	http.ListenAndServe(":8090", nil)
}
