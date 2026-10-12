package com.algotrade.algotradebackend.broker.zerodha;

import org.json.JSONObject;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.io.ByteArrayOutputStream;
import java.net.CookieManager;
import java.net.CookiePolicy;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Headless Automated Authentication Service for Zerodha Kite Connect.
 * 
 * <p>Automates the daily 2FA login handshake:
 * 1. POST /api/login with User ID and Password -> receives request_id
 * 2. Computes the current 6-digit TOTP code using standard RFC 6238 algorithm
 * 3. POST /api/twofa with request_id and generated TOTP code -> receives authenticated cookies
 * 4. GET /connect/login with authenticated cookies -> captures the 302 Location header containing request_token</p>
 */
@Service
public class KiteAutoLoginService {

    private static final Logger log = LoggerFactory.getLogger(KiteAutoLoginService.class);
    private static final String BASE32_CHARS = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

    /**
     * Executes the complete automated headless login flow and returns the extracted request_token.
     *
     * @param userId      Zerodha User ID (e.g. LBN796)
     * @param password    Zerodha Password
     * @param totpSecret  Base32 TOTP Secret Key (from Kite 2FA setup)
     * @param apiKey      Kite Connect API Key
     * @return Fresh request_token string ready for generateSession
     */
    public String performAutoLogin(String userId, String password, String totpSecret, String apiKey) throws Exception {
        if (userId == null || userId.isBlank()) {
            throw new IllegalArgumentException("Zerodha User ID is required for auto-login.");
        }
        if (password == null || password.isBlank()) {
            throw new IllegalArgumentException("Zerodha Password is required for auto-login.");
        }
        if (totpSecret == null || totpSecret.isBlank()) {
            throw new IllegalArgumentException("Zerodha TOTP Secret is required for auto-login.");
        }

        log.info("Initiating automated headless Kite login for user: {}", userId);

        CookieManager cookieManager = new CookieManager(null, CookiePolicy.ACCEPT_ALL);
        HttpClient client = HttpClient.newBuilder()
                .cookieHandler(cookieManager)
                .followRedirects(HttpClient.Redirect.NEVER) // Intercept the 302 redirect location
                .connectTimeout(Duration.ofSeconds(15))
                .build();

        // Step 1: POST https://kite.zerodha.com/api/login
        String loginFormData = "user_id=" + URLEncoder.encode(userId.trim(), StandardCharsets.UTF_8)
                + "&password=" + URLEncoder.encode(password.trim(), StandardCharsets.UTF_8);

        HttpRequest loginRequest = HttpRequest.newBuilder()
                .uri(URI.create("https://kite.zerodha.com/api/login"))
                .header("Content-Type", "application/x-www-form-urlencoded")
                .header("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
                .POST(HttpRequest.BodyPublishers.ofString(loginFormData))
                .build();

        HttpResponse<String> loginResponse = client.send(loginRequest, HttpResponse.BodyHandlers.ofString());
        if (loginResponse.statusCode() != 200) {
            throw new IllegalStateException("Kite login endpoint rejected request (HTTP " + loginResponse.statusCode() + "): " + loginResponse.body());
        }

        JSONObject loginJson = new JSONObject(loginResponse.body());
        if (!"success".equalsIgnoreCase(loginJson.optString("status"))) {
            throw new IllegalStateException("Kite login failed: " + loginJson.optString("message", loginResponse.body()));
        }

        String requestId = loginJson.getJSONObject("data").getString("request_id");
        log.info("Kite Step 1 Success: Received request_id for user {}", userId);

        // Step 2: Handle TOTP - If user passed dynamic 6-digit OTP directly, use it; otherwise compute from Base32 secret
        String cleanTotp = totpSecret.trim().replaceAll("\\s+", "");
        String totpCode;
        if (cleanTotp.matches("^\\d{6}$")) {
            totpCode = cleanTotp;
            log.info("Kite Step 2: User provided 6-digit dynamic TOTP code directly from authenticator");
        } else {
            totpCode = generateTotp(cleanTotp);
            log.info("Kite Step 2: Computed 6-digit TOTP code from Base32 secret key");
        }

        String twofaFormData = "user_id=" + URLEncoder.encode(userId.trim(), StandardCharsets.UTF_8)
                + "&request_id=" + URLEncoder.encode(requestId, StandardCharsets.UTF_8)
                + "&twofa_value=" + URLEncoder.encode(totpCode, StandardCharsets.UTF_8)
                + "&twofa_type=totp&skip_session=";

        HttpRequest twofaRequest = HttpRequest.newBuilder()
                .uri(URI.create("https://kite.zerodha.com/api/twofa"))
                .header("Content-Type", "application/x-www-form-urlencoded")
                .header("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
                .POST(HttpRequest.BodyPublishers.ofString(twofaFormData))
                .build();

        HttpResponse<String> twofaResponse = client.send(twofaRequest, HttpResponse.BodyHandlers.ofString());
        if (twofaResponse.statusCode() != 200) {
            throw new IllegalStateException("Kite 2FA endpoint rejected request (HTTP " + twofaResponse.statusCode() + "): " + twofaResponse.body());
        }

        JSONObject twofaJson = new JSONObject(twofaResponse.body());
        if (!"success".equalsIgnoreCase(twofaJson.optString("status"))) {
            throw new IllegalStateException("Kite 2FA failed: " + twofaJson.optString("message", twofaResponse.body()));
        }
        log.info("Kite Step 2 Success: 2FA TOTP validated successfully. Session cookies established.");

        // Step 3: Follow OAuth redirect chain to get request_token (e.g. /connect/login -> /connect/finish -> redirect_url?request_token=...)
        String connectLoginUrl = "https://kite.trade/connect/login?api_key=" + URLEncoder.encode(apiKey.trim(), StandardCharsets.UTF_8) + "&v=3";
        URI currentUri = URI.create(connectLoginUrl);
        String finalRedirectUrl = null;
        int maxHops = 10;

        for (int hop = 0; hop < maxHops; hop++) {
            HttpRequest connectRequest = HttpRequest.newBuilder()
                    .uri(currentUri)
                    .header("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
                    .GET()
                    .build();

            HttpResponse<String> connectResponse = client.send(connectRequest, HttpResponse.BodyHandlers.ofString());
            String locationHeader = connectResponse.headers().firstValue("Location").orElse(null);
            log.info("Kite Redirect Hop #{}: status={}, Location={}", hop + 1, connectResponse.statusCode(), locationHeader);

            if (locationHeader == null || locationHeader.isBlank()) {
                break;
            }

            finalRedirectUrl = locationHeader;

            if (locationHeader.contains("request_token=")) {
                log.info("Kite Step 3: Successfully reached redirect with request_token at hop #{}", hop + 1);
                break;
            }

            // Resolve next hop URI (handles relative and absolute URLs)
            currentUri = currentUri.resolve(locationHeader);
        }

        if (finalRedirectUrl == null || !finalRedirectUrl.contains("request_token=")) {
            throw new IllegalStateException("Did not receive request_token in redirect Location header: " + finalRedirectUrl);
        }

        String requestToken = extractParam(finalRedirectUrl, "request_token");
        if (requestToken == null || requestToken.isBlank()) {
            throw new IllegalStateException("Could not extract request_token parameter from redirect URL: " + finalRedirectUrl);
        }

        log.info("Kite Step 3 Success: Extracted fresh request_token via automated handshake");
        return requestToken;
    }

    /**
     * Computes the current 6-digit Time-based One-Time Password (TOTP) from a Base32 secret
     * following the standard RFC 6238 / RFC 4226 specification.
     */
    public String generateTotp(String base32Secret) {
        long timeStepSeconds = 30;
        long currentInterval = (System.currentTimeMillis() / 1000) / timeStepSeconds;
        byte[] key = decodeBase32(base32Secret.trim().replace(" ", "").toUpperCase());
        return computeTotpCode(key, currentInterval, 6);
    }

    private byte[] decodeBase32(String secret) {
        String clean = secret.trim().replace(" ", "").replace("=", "").toUpperCase();
        int bits = 0;
        int value = 0;
        ByteArrayOutputStream output = new ByteArrayOutputStream();
        for (char c : clean.toCharArray()) {
            int val = BASE32_CHARS.indexOf(c);
            if (val < 0) continue;
            value = (value << 5) | val;
            bits += 5;
            if (bits >= 8) {
                bits -= 8;
                output.write((value >> bits) & 0xFF);
            }
        }
        return output.toByteArray();
    }

    private String computeTotpCode(byte[] key, long timeInterval, int digits) {
        try {
            Mac mac = Mac.getInstance("HmacSHA1");
            mac.init(new SecretKeySpec(key, "RAW"));
            byte[] data = new byte[8];
            long value = timeInterval;
            for (int i = 7; i >= 0; i--) {
                data[i] = (byte) (value & 0xFF);
                value >>= 8;
            }
            byte[] hash = mac.doFinal(data);
            int offset = hash[hash.length - 1] & 0x0F;
            long truncatedHash = 0;
            for (int i = 0; i < 4; ++i) {
                truncatedHash <<= 8;
                truncatedHash |= (hash[offset + i] & 0xFF);
            }
            truncatedHash &= 0x7FFFFFFF;
            truncatedHash %= (long) Math.pow(10, digits);
            return String.format("%0" + digits + "d", truncatedHash);
        } catch (Exception e) {
            throw new RuntimeException("Failed to compute HMAC-SHA1 TOTP: " + e.getMessage(), e);
        }
    }

    private String extractParam(String url, String paramName) {
        Pattern pattern = Pattern.compile("[?&]" + Pattern.quote(paramName) + "=([^&#]+)");
        Matcher matcher = pattern.matcher(url);
        if (matcher.find()) {
            return matcher.group(1);
        }
        return null;
    }
}
