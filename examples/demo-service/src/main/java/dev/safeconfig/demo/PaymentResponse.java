package dev.safeconfig.demo;

import java.time.Instant;

record PaymentResponse(
        String orderId,
        boolean authorized,
        double failureRate,
        int configVersion,
        String configSource,
        Instant processedAt
) {
}
