package dev.safeconfig.demo;

import java.time.Instant;
import io.micrometer.core.instrument.Counter;
import io.micrometer.core.instrument.MeterRegistry;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class PaymentController {
    private final PaymentSettingsProvider settingsProvider;
    private final MeterRegistry meterRegistry;

    PaymentController(PaymentSettingsProvider settingsProvider, MeterRegistry meterRegistry) {
        this.settingsProvider = settingsProvider;
        this.meterRegistry = meterRegistry;
    }

    @GetMapping("/v1/payments/authorize")
    PaymentResponse authorize(@RequestParam(defaultValue = "demo-order") String orderId) {
        PaymentSettings settings = settingsProvider.current();
        boolean failed = shouldFail(orderId, settings.failureRate());
        recordRequest(settings, failed);
        return new PaymentResponse(
                orderId,
                !failed,
                settings.failureRate(),
                settings.configVersion(),
                settings.source(),
                Instant.now()
        );
    }

    private void recordRequest(PaymentSettings settings, boolean failed) {
        Counter.builder("payment_requests")
                .description("Payment authorization requests processed by the demo service")
                .tag("config_version", Integer.toString(settings.configVersion()))
                .tag("result", failed ? "error" : "success")
                .register(meterRegistry)
                .increment();
    }

    private boolean shouldFail(String orderId, double failureRate) {
        if (failureRate <= 0) {
            return false;
        }
        if (failureRate >= 1) {
            return true;
        }

        int bucket = Math.floorMod(orderId.hashCode(), 10_000);
        return bucket < Math.round(failureRate * 10_000);
    }
}
