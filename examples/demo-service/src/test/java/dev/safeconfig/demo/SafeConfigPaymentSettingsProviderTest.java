package dev.safeconfig.demo;

import static org.assertj.core.api.Assertions.assertThat;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.URI;
import java.time.Duration;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

class SafeConfigPaymentSettingsProviderTest {
    private HttpServer server;

    @AfterEach
    void stopServer() {
        if (server != null) {
            server.stop(0);
        }
    }

    @Test
    void readsFailureRateFromLocalAgent() throws Exception {
        startServer(200, "{\"key\":\"payment.failure_rate\",\"version\":3,\"value\":0.25}");

        PaymentSettings settings = provider(serverUrl()).current();

        assertThat(settings.failureRate()).isEqualTo(0.25);
        assertThat(settings.configVersion()).isEqualTo(3);
        assertThat(settings.source()).isEqualTo("safeconfig");
    }

    @Test
    void fallsBackWhenLocalAgentIsUnavailable() {
        PaymentSettings settings = provider(URI.create("http://127.0.0.1:1")).current();

        assertThat(settings.failureRate()).isEqualTo(0.1);
        assertThat(settings.configVersion()).isZero();
        assertThat(settings.source()).isEqualTo("fallback");
    }

    private SafeConfigPaymentSettingsProvider provider(URI agentUrl) {
        return new SafeConfigPaymentSettingsProvider(
                new ObjectMapper(),
                agentUrl,
                "payment.failure_rate",
                0.1,
                Duration.ofMillis(200)
        );
    }

    private void startServer(int status, String body) throws IOException {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/v1/config/payment.failure_rate", exchange -> {
            byte[] response = body.getBytes();
            exchange.getResponseHeaders().add("Content-Type", "application/json");
            exchange.sendResponseHeaders(status, response.length);
            exchange.getResponseBody().write(response);
            exchange.close();
        });
        server.start();
    }

    private URI serverUrl() {
        return URI.create("http://127.0.0.1:" + server.getAddress().getPort());
    }
}
