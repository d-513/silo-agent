import { eq } from "../testing.ts";
import { deviceName, methodName } from "./device.ts";

eq(deviceName("Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:131.0) Gecko/20100101 Firefox/131.0"), "Firefox on macOS", "Firefox");
eq(deviceName("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36"), "Chrome on Windows", "Chrome says Safari too");
eq(deviceName("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36 Edg/129.0.0.0"), "Edge on Windows", "Edge says Chrome too");
eq(deviceName("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"), "Safari on macOS", "Safari");
eq(deviceName("Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"), "Safari on iOS", "iPhone is iOS, not macOS");
eq(deviceName("Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36"), "Chrome on Android", "Android is not Linux");
eq(deviceName("Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0"), "Firefox on Linux", "Linux");
eq(deviceName("Silo/1 CFNetwork/1568.100.1 Darwin/24.0.0"), "Silo for iOS", "the iOS app");
eq(deviceName("curl/8.7.1"), "Another app", "something else");
eq(deviceName(""), "Unknown device", "nothing recorded");

eq(methodName("password"), "Password", "password");
eq(methodName("oidc"), "Single sign-on", "oidc");
eq(methodName("invite"), "Invite link", "invite");
eq(methodName(""), "Signed in", "a session from before methods were recorded");
