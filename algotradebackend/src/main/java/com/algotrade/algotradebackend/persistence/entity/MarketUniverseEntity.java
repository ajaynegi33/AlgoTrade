package com.algotrade.algotradebackend.persistence.entity;

import jakarta.persistence.*;
import java.time.Instant;

/**
 * JPA Entity storing the dynamically discovered stock market universe.
 * Refreshed daily from the Zerodha Kite Master Instruments stream at 08:30 AM IST.
 */
@Entity
@Table(name = "market_universe", indexes = {
    @Index(name = "idx_universe_symbol", columnList = "symbol"),
    @Index(name = "idx_universe_is_fno", columnList = "isFno")
})
public class MarketUniverseEntity {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(nullable = false, unique = true)
    private String symbol;

    private String companyName;

    private String exchange;

    private String segment;

    private Long instrumentToken;

    @Column(nullable = false)
    private boolean isFno;

    private Integer lotSize;

    private Double tickSize;

    @Column(nullable = false)
    private Instant lastSyncedAt;

    public MarketUniverseEntity() {}

    public MarketUniverseEntity(
        String symbol,
        String companyName,
        String exchange,
        String segment,
        Long instrumentToken,
        boolean isFno,
        Integer lotSize,
        Double tickSize,
        Instant lastSyncedAt
    ) {
        this.symbol = symbol;
        this.companyName = companyName;
        this.exchange = exchange;
        this.segment = segment;
        this.instrumentToken = instrumentToken;
        this.isFno = isFno;
        this.lotSize = lotSize;
        this.tickSize = tickSize;
        this.lastSyncedAt = lastSyncedAt;
    }

    public Long getId() { return id; }
    public void setId(Long id) { this.id = id; }

    public String getSymbol() { return symbol; }
    public void setSymbol(String symbol) { this.symbol = symbol; }

    public String getCompanyName() { return companyName; }
    public void setCompanyName(String companyName) { this.companyName = companyName; }

    public String getExchange() { return exchange; }
    public void setExchange(String exchange) { this.exchange = exchange; }

    public String getSegment() { return segment; }
    public void setSegment(String segment) { this.segment = segment; }

    public Long getInstrumentToken() { return instrumentToken; }
    public void setInstrumentToken(Long instrumentToken) { this.instrumentToken = instrumentToken; }

    public boolean isFno() { return isFno; }
    public void setFno(boolean fno) { isFno = fno; }

    public Integer getLotSize() { return lotSize; }
    public void setLotSize(Integer lotSize) { this.lotSize = lotSize; }

    public Double getTickSize() { return tickSize; }
    public void setTickSize(Double tickSize) { this.tickSize = tickSize; }

    public Instant getLastSyncedAt() { return lastSyncedAt; }
    public void setLastSyncedAt(Instant lastSyncedAt) { this.lastSyncedAt = lastSyncedAt; }
}
