package com.algotrade.algotradebackend.scanner;

import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.persistence.entity.MarketUniverseEntity;
import com.algotrade.algotradebackend.persistence.repository.MarketUniverseRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;

import java.util.List;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.anyList;
import static org.mockito.Mockito.verify;

class InstrumentMasterServiceTest {

    private MarketUniverseRepository universeRepository;
    private EngineActivityFeedService activityFeedService;
    private InstrumentMasterService service;

    @BeforeEach
    void setUp() {
        universeRepository = Mockito.mock(MarketUniverseRepository.class);
        activityFeedService = Mockito.mock(EngineActivityFeedService.class);
        service = new InstrumentMasterService(universeRepository, activityFeedService);
    }

    @Test
    void testParseCsvRow() {
        String sampleLine = "738561,2885,RELIANCE,\"RELIANCE INDUSTRIES\",0,,0,0.05,1,EQ,NSE,NSE";
        List<String> row = InstrumentMasterService.parseCsvRow(sampleLine);

        assertEquals(12, row.size());
        assertEquals("738561", row.get(0));
        assertEquals("RELIANCE", row.get(2));
        assertEquals("RELIANCE INDUSTRIES", row.get(3));
        assertEquals("0.05", row.get(7));
        assertEquals("1", row.get(8));
        assertEquals("EQ", row.get(9));
        assertEquals("NSE", row.get(10));
        assertEquals("NSE", row.get(11));
    }

    @Test
    void testParseCsvRowWithQuotesAndSpacing() {
        String sampleLine = "12468226,48704,NIFTY26OCTFUT,\"NIFTY\",0,2026-10-27,0,0.1,65,FUT,NFO-FUT,NFO";
        List<String> row = InstrumentMasterService.parseCsvRow(sampleLine);

        assertEquals(12, row.size());
        assertEquals("NIFTY26OCTFUT", row.get(2));
        assertEquals("NIFTY", row.get(3));
        assertEquals("FUT", row.get(9));
        assertEquals("NFO-FUT", row.get(10));
    }

    @Test
    void testPersistUniverseTransactional() {
        List<MarketUniverseEntity> testList = List.of(
            new MarketUniverseEntity("RELIANCE", "RELIANCE IND", "NSE", "NSE", 738561L, true, 1, 0.05, java.time.Instant.now()),
            new MarketUniverseEntity("INFY", "INFOSYS", "NSE", "NSE", 408065L, true, 1, 0.05, java.time.Instant.now())
        );

        service.persistUniverse(testList);

        verify(universeRepository).deleteAll();
        verify(universeRepository).saveAll(testList);
    }
}
