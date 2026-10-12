package com.algotrade.algotradebackend.broker.zerodha;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

public class KiteAutoLoginServiceTest {

    @Test
    public void testTotpGenerationFormat() {
        KiteAutoLoginService service = new KiteAutoLoginService();
        // Standard test Base32 secret
        String secret = "JBSWY3DPEHPK3PXP";
        String totp = service.generateTotp(secret);
        assertNotNull(totp);
        assertEquals(6, totp.length(), "TOTP code must be exactly 6 digits");
        assertTrue(totp.matches("\\d{6}"), "TOTP code must contain only numerical digits");
    }
}
