1. In the [Azure portal](https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade), open **App registrations → New registration**.
2. Supported account types: **Accounts in any organizational directory and personal Microsoft accounts**.
3. Redirect URI: platform **Web**, `{public_url}/oauth/callback`.
4. Under **Certificates & secrets**, create a client secret.
5. Under **API permissions**, add Microsoft Graph delegated `Files.ReadWrite.All`, `Sites.Read.All`, `User.Read`, `offline_access`.
6. Paste the Application (client) ID and the secret **value** here.
