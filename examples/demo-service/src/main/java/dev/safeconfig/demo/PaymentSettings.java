package dev.safeconfig.demo;

record PaymentSettings(
        double failureRate,
        int configVersion,
        String source
) {
}
