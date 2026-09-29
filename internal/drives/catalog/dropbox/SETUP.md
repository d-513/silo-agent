1. Create an app in the [Dropbox App Console](https://www.dropbox.com/developers/apps): **Scoped access**, **Full Dropbox**.
2. On **Permissions**, tick `account_info.read`, `files.metadata.read`, `files.metadata.write`, `files.content.read`, `files.content.write`, then **Submit**.
3. On **Settings**, add the redirect URI `{public_url}/oauth/callback`.
4. Paste the App key (client ID) and App secret here.
