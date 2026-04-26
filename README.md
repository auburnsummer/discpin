# Discord Pinner

Discord Pinner is a proxy to a Discord webhook that also pins the message. You can use it
in-place of a normal Discord webhook if you need the posted message to be pinned.

Use the following query parameters:

 - `url`: The original Discord webhook
 - `token`: An _encrypted_ bot token with Pin Messages, View Channels, and Read Message History
   - These are the only three scopes needed. If you will never use `remove_previous` you can get away
     with only Pin Messages
   - See below section for how to produce the encrypted token
 - `remove_previous`: If true (remove_previous=true), it will delete the previously pinned message
   from the same webhook.

## How to encrypt the token

1. Go to https://discpin.auburn.dev/pubkey and copy the result
2. Go to this page: https://emn178.github.io/online-tools/rsa/encrypt/
   1. Set "Algorithm" to "ECB / OAEP / SHA256"
   2. Set "Output" to "Hex (Upper Case)"
3. Paste the public key from step 1 into the "Public Key" field
4. Paste your Discord bot token into the "Input" field
5. The "Output" will show the encrypted token.