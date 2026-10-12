package com.algotrade.algotradebackend.core.service;

import com.algotrade.algotradebackend.core.model.EngineActivityEvent;
import org.junit.jupiter.api.Test;

import java.util.List;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.*;

class EngineActivityFeedServiceTest {

    @Test
    void testRecordAndBoundedCapacity() {
        EngineActivityFeedService service = new EngineActivityFeedService();
        // Initial boot event is recorded
        assertEquals(1, service.getBufferSize());

        // Record 250 events to test 200-capacity ring buffer eviction
        for (int i = 0; i < 250; i++) {
            service.record("STRATEGY", "INFO", "Candidate Evaluation", "Evaluating step " + i, "TEST", Map.of("step", i));
        }

        assertEquals(200, service.getBufferSize());
        List<EngineActivityEvent> recent = service.getRecentEvents(null, 10);
        assertEquals(10, recent.size());
        // Verify reverse chronological order (newest first)
        assertTrue(recent.get(0).id() > recent.get(1).id());
    }

    @Test
    void testIncrementalSinceIdQuery() {
        EngineActivityFeedService service = new EngineActivityFeedService();
        EngineActivityEvent e1 = service.record("STRATEGY", "INFO", "Event 1", "Desc 1");
        EngineActivityEvent e2 = service.record("ORDERS", "INFO", "Event 2", "Desc 2");
        EngineActivityEvent e3 = service.record("RISK", "WARN", "Event 3", "Desc 3");

        // Query events since e1.id()
        List<EngineActivityEvent> delta = service.getRecentEvents(e1.id(), 50);
        assertEquals(2, delta.size());
        assertEquals(e3.id(), delta.get(0).id());
        assertEquals(e2.id(), delta.get(1).id());
    }

    @Test
    void testExecutionModeAndTradeId() {
        EngineActivityFeedService service = new EngineActivityFeedService();
        EngineActivityEvent evt = service.record(
            "ORDERS",
            "SUCCESS",
            "LIVE",
            "TRD-2026-10-12-RELIANCE",
            "Entry Order Dispatched",
            "BUY 10 x RELIANCE",
            "RELIANCE"
        );
        assertEquals("LIVE", evt.executionMode());
        assertEquals("TRD-2026-10-12-RELIANCE", evt.tradeId());
        assertEquals("RELIANCE", evt.symbol());
    }
}
