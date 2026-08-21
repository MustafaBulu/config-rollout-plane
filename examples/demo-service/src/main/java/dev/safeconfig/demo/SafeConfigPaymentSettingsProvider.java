package dev.safeconfig.demo;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

@Component
class SafeConfigPaymentSettingsProvider implements PaymentSettingsProvider {
    private static final String FALLBACK_SOURCE = "fallback";
    private static final String SAFECONFIG_SOURCE = "safeconfig";

    private final HttpClient httpClient;
    private final ObjectMapper objectMapper;
    private final URI agentUrl;
    private final String failureRateKey;
    private final double defaultFailureRate;
    private final Duration requestTimeout;

    SafeConfigPaymentSettingsProvider(
            ObjectMapper objectMapper,
            @Value("${safeconfig.agent-url:http://localhost:8082}") URI agentUrl,
            @Value("${safeconfig.failure-rate-key:payment.failure_rate}") String failureRateKey,
            @Value("${safeconfig.default-failure-rate:0.0}") double defaultFailureRate,
            @Value("${safeconfig.request-timeout:500ms}") Duration requestTimeout
    ) {
        this.objectMapper = objectMapper;
        this.agentUrl = agentUrl;
        this.failureRateKey = failureRateKey;
        this.defaultFailureRate = clamp(defaultFailureRate);
        this.requestTimeout = requestTimeout;
        this.httpClient = HttpClient.newBuilder()
                .connectTimeout(requestTimeout)
                .build();
    }

    @Override
    public PaymentSettings current() {
        try {
            AgentConfigResponse response = fetchConfig();
            return new PaymentSettings(clamp(response.value().asDouble(defaultFailureRate)), response.version(), SAFECONFIG_SOURCE);
        } catch (IOException | InterruptedException | RuntimeException err) {
            if (err instanceof InterruptedException) {
                Thread.currentThread().interrupt();
            }
            return new PaymentSettings(defaultFailureRate, 0, FALLBACK_SOURCE);
        }
    }

    private AgentConfigResponse fetchConfig() throws IOException, InterruptedException {
        URI endpoint = agentUrl.resolve("/v1/config/" + URLEncoder.encode(failureRateKey, StandardCharsets.UTF_8));
        HttpRequest request = HttpRequest.newBuilder(endpoint)
                .timeout(requestTimeout)
                .GET()
                .build();
        HttpResponse<String> response = httpClient.send(request, HttpResponse.BodyHandlers.ofString());
        if (response.statusCode() != 200) {
            throw new IOException("safeconfig agent returned status " + response.statusCode());
        }
        return objectMapper.readValue(response.body(), AgentConfigResponse.class);
    }

    private static double clamp(double value) {
        if (Double.isNaN(value) || value < 0) {
            return 0;
        }
        if (value > 1) {
            return 1;
        }
        return value;
    }

    private record AgentConfigResponse(String key, int version, JsonNode value) {
    }
}
