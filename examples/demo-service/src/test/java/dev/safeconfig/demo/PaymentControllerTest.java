package dev.safeconfig.demo;

import static org.assertj.core.api.Assertions.assertThat;

import io.micrometer.core.instrument.simple.SimpleMeterRegistry;
import org.junit.jupiter.api.Test;

class PaymentControllerTest {
    @Test
    void authorizesDemoPayment() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        PaymentResponse response = new PaymentController(() -> new PaymentSettings(0, 0, "test"), registry)
                .authorize("order-1");

        assertThat(response).isNotNull();
        assertThat(response.orderId()).isEqualTo("order-1");
        assertThat(response.authorized()).isTrue();
        assertThat(response.failureRate()).isZero();
        assertThat(response.configVersion()).isZero();
        assertThat(response.configSource()).isEqualTo("test");
        assertThat(registry.counter("payment_requests", "config_version", "0", "result", "success").count())
                .isEqualTo(1);
    }

    @Test
    void rejectsWhenFailureRateIsOne() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        PaymentResponse response = new PaymentController(() -> new PaymentSettings(1, 2, "test"), registry)
                .authorize("order-1");

        assertThat(response.authorized()).isFalse();
        assertThat(response.failureRate()).isEqualTo(1);
        assertThat(response.configVersion()).isEqualTo(2);
        assertThat(registry.counter("payment_requests", "config_version", "2", "result", "error").count())
                .isEqualTo(1);
    }
}
