1. In the [Google Cloud console](https://console.cloud.google.com/apis/credentials), enable the **Google Drive API** for a project.
2. Configure the OAuth consent screen (External is fine; add yourself as a test user while it is in testing).
3. Create an **OAuth client ID** of type **Web application** and add this authorized redirect URI:
   `{public_url}/oauth/callback`
4. Paste the client ID and secret here.
