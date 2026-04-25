package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

var netClient = &http.Client{
	Timeout: 10 * time.Second,
}

var privateKey *rsa.PrivateKey

func loadPrivateKey() error {
	pemData := os.Getenv("DISCPIN_PRIVATE_KEY")
	if pemData == "" {
		return fmt.Errorf("DISCPIN_PRIVATE_KEY environment variable is not set")
	}

	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return fmt.Errorf("failed to decode PEM block from DISCPIN_PRIVATE_KEY")
	}

	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %v (expected PKCS#1 PEM from: ssh-keygen -t rsa -m PEM)", err)
	}
	privateKey = rsaKey
	return nil
}

// decryptToken decrypts a hex-encoded RSA-OAEP (SHA-256) ciphertext.
func decryptToken(encryptedHex string) (string, error) {
	ciphertext, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", fmt.Errorf("failed to hex-decode token")
	}

	plaintext, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt token")
	}
	return string(plaintext), nil
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

func pubkey(w http.ResponseWriter, r *http.Request) {
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		http.Error(w, "Failed to marshal public key", http.StatusInternalServerError)
		return
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})
	w.Header().Set("Content-Type", "text/plain")
	w.Write(pemBlock)
}

func exec(w http.ResponseWriter, r *http.Request) {
	var url = r.URL.Query().Get("url")
	var encryptedToken = r.URL.Query().Get("token")
	var remove_previous = r.URL.Query().Get("remove_previous")

	if url == "" || encryptedToken == "" {
		http.Error(w, "Missing url or token", http.StatusBadRequest)
		return
	}
	if !isDiscordWebhookURL(url) {
		http.Error(w, "Invalid Discord webhook URL", http.StatusBadRequest)
		return
	}

	token, err := decryptToken(encryptedToken)
	if err != nil {
		http.Error(w, "Invalid token: "+err.Error(), http.StatusBadRequest)
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

	if remove_previous == "true" {
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
	}

	fmt.Fprintf(w, "Message sent successfully! Message ID: %s\n", responseData.Id)
}

func home(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Welcome to Discpin! Use /pubkey to get the public key and /exec to execute a command.")
}

func main() {
	if err := loadPrivateKey(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading private key: %v\n", err)
		os.Exit(1)
	}

	http.HandleFunc("/pubkey", pubkey)
	http.HandleFunc("/exec", exec)

	http.HandleFunc("/", home)

	http.ListenAndServe(":8090", nil)
}
