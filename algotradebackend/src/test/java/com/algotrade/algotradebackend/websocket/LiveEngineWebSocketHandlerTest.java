package com.algotrade.algotradebackend.websocket;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.EngineActivityEvent;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.strategy.StrategyManager;
import com.algotrade.algotradebackend.strategy.TradingStrategy;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.mockito.Mockito;
import org.springframework.web.socket.CloseStatus;
import org.springframework.web.socket.TextMessage;
import org.springframework.web.socket.WebSocketSession;

import java.io.IOException;
import java.time.Instant;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

class LiveEngineWebSocketHandlerTest {

    private BrokerGatewayManager brokerGatewayManager;
    private StrategyManager strategyManager;
    private PositionSizingEngine positionSizingEngine;
    private MarketCalendarService marketCalendarService;
    private EngineActivityFeedService activityFeedService;
    private LiveEngineWebSocketHandler handler;

    @BeforeEach
    void setUp() {
        brokerGatewayManager = mock(BrokerGatewayManager.class);
        strategyManager = mock(StrategyManager.class);
        positionSizingEngine = mock(PositionSizingEngine.class);
        marketCalendarService = mock(MarketCalendarService.class);
        activityFeedService = mock(EngineActivityFeedService.class);

        BrokerGateway dummyGateway = mock(BrokerGateway.class);
        when(brokerGatewayManager.getActiveMode()).thenReturn(ExecutionMode.PAPER);
        when(brokerGatewayManager.getActiveGateway()).thenReturn(dummyGateway);
        when(dummyGateway.getOpenPosition()).thenReturn(Optional.empty());

        TradingStrategy dummyStrategy = mock(TradingStrategy.class);
        when(dummyStrategy.getStrategyId()).thenReturn("MOMENTUM_BREAKOUT");
        when(dummyStrategy.getStrategyName()).thenReturn("Momentum Breakout");
        when(strategyManager.getActiveStrategy()).thenReturn(dummyStrategy);
        when(strategyManager.getLatestTopCandidates()).thenReturn(List.of());

        when(positionSizingEngine.getCapital()).thenReturn(50000.0);
        when(positionSizingEngine.getRiskBudget()).thenReturn(1000.0);
        when(positionSizingEngine.canTradeToday()).thenReturn(true);

        when(marketCalendarService.isMarketOpen()).thenReturn(true);
        when(marketCalendarService.getNextMarketOpenFormatted()).thenReturn("Tomorrow 09:15 AM");

        when(activityFeedService.getRecentEvents(anyInt())).thenReturn(List.of());

        handler = new LiveEngineWebSocketHandler(
            brokerGatewayManager,
            strategyManager,
            positionSizingEngine,
            marketCalendarService,
            activityFeedService,
            null
        );
    }

    @Test
    void testConnectionEstablishedSendsInitialSnapshot() throws IOException {
        WebSocketSession session = mock(WebSocketSession.class);
        when(session.getId()).thenReturn("sess-1");
        when(session.isOpen()).thenReturn(true);

        handler.afterConnectionEstablished(session);

        assertEquals(1, handler.getActiveSessionCount());

        ArgumentCaptor<TextMessage> captor = ArgumentCaptor.forClass(TextMessage.class);
        verify(session, atLeastOnce()).sendMessage(captor.capture());

        String sentPayload = captor.getValue().getPayload();
        assertTrue(sentPayload.contains("\"type\":\"INITIAL_SNAPSHOT\""));
        assertTrue(sentPayload.contains("\"capital\":50000.0"));
        assertTrue(sentPayload.contains("\"mode\":\"PAPER\""));
    }

    @Test
    void testPingRespondsWithPong() throws IOException {
        WebSocketSession session = mock(WebSocketSession.class);
        when(session.getId()).thenReturn("sess-2");
        when(session.isOpen()).thenReturn(true);

        handler.afterConnectionEstablished(session);
        reset(session);
        when(session.isOpen()).thenReturn(true);

        handler.handleTextMessage(session, new TextMessage("{\"type\":\"PING\"}"));

        ArgumentCaptor<TextMessage> captor = ArgumentCaptor.forClass(TextMessage.class);
        verify(session).sendMessage(captor.capture());
        assertTrue(captor.getValue().getPayload().contains("\"type\":\"PONG\""));
    }

    @Test
    void testConnectionClosedRemovesSession() {
        WebSocketSession session = mock(WebSocketSession.class);
        when(session.getId()).thenReturn("sess-3");
        when(session.isOpen()).thenReturn(true);

        handler.afterConnectionEstablished(session);
        assertEquals(1, handler.getActiveSessionCount());

        handler.afterConnectionClosed(session, CloseStatus.NORMAL);
        assertEquals(0, handler.getActiveSessionCount());
    }

    @Test
    void testBroadcastDispatchesToAllActiveSessions() throws IOException {
        WebSocketSession s1 = mock(WebSocketSession.class);
        when(s1.getId()).thenReturn("sess-a");
        when(s1.isOpen()).thenReturn(true);

        WebSocketSession s2 = mock(WebSocketSession.class);
        when(s2.getId()).thenReturn("sess-b");
        when(s2.isOpen()).thenReturn(true);

        handler.afterConnectionEstablished(s1);
        handler.afterConnectionEstablished(s2);

        reset(s1, s2);
        when(s1.isOpen()).thenReturn(true);
        when(s2.isOpen()).thenReturn(true);

        String testMsg = "{\"type\":\"TEST_BROADCAST\"}";
        handler.broadcast(testMsg);

        ArgumentCaptor<TextMessage> c1 = ArgumentCaptor.forClass(TextMessage.class);
        ArgumentCaptor<TextMessage> c2 = ArgumentCaptor.forClass(TextMessage.class);

        verify(s1).sendMessage(c1.capture());
        verify(s2).sendMessage(c2.capture());

        assertEquals(testMsg, c1.getValue().getPayload());
        assertEquals(testMsg, c2.getValue().getPayload());
    }

    @Test
    void testLiveWebSocketBroadcastService() {
        LiveWebSocketBroadcastService broadcastService = new LiveWebSocketBroadcastService(handler);

        // When 0 sessions, broadcast does not throw and skips
        EngineActivityEvent dummyEvent = new EngineActivityEvent(
            1L, Instant.now(), "SYSTEM", "INFO", "PAPER", null, "Test", "Msg", "SBIN", Collections.emptyMap()
        );
        assertDoesNotThrow(() -> broadcastService.broadcastDecisionEvent(dummyEvent));
        assertDoesNotThrow(() -> broadcastService.broadcastOverview());
    }
}
