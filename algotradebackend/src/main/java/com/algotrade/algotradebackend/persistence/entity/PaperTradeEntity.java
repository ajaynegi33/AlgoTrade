package com.algotrade.algotradebackend.persistence.entity;

import jakarta.persistence.*;
import java.time.Instant;

/**
 * JPA Entity representing an executed and settled paper trade.
 * Persisted in the dedicated `algotradepaper.db` SQLite database file.
 * 
 * <p>Stores complete financial auditing details including every breakdown item
 * of Indian equity statutory charges (Brokerage, STT, Exchange, SEBI, Stamp Duty, GST),
 * allowing full transparency into true net profitability.</p>
 */
@Entity
@Table(name = "paper_trades")
public class PaperTradeEntity {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(nullable = false)
    private String symbol;

    @Column(nullable = false)
    private Integer quantity;

    @Column(nullable = false)
    private Double buyPrice;

    @Column(nullable = false)
    private Double sellPrice;

    @Column(nullable = false)
    private Instant entryTime;

    @Column(nullable = false)
    private Instant exitTime;

    @Column(nullable = false)
    private String exitReason;

    @Column(nullable = false)
    private Double grossPnL;

    @Column(nullable = false)
    private Double netPnL;

    @Column(nullable = false)
    private Double totalCharges;

    @Column(nullable = false)
    private Double brokerage;

    @Column(nullable = false)
    private Double stt;

    @Column(nullable = false)
    private Double exchangeCharges;

    @Column(nullable = false)
    private Double sebiCharges;

    @Column(nullable = false)
    private Double stampDuty;

    @Column(nullable = false)
    private Double gst;

    public PaperTradeEntity() {}

    public PaperTradeEntity(
        String symbol,
        Integer quantity,
        Double buyPrice,
        Double sellPrice,
        Instant entryTime,
        Instant exitTime,
        String exitReason,
        Double grossPnL,
        Double netPnL,
        Double totalCharges,
        Double brokerage,
        Double stt,
        Double exchangeCharges,
        Double sebiCharges,
        Double stampDuty,
        Double gst
    ) {
        this.symbol = symbol;
        this.quantity = quantity;
        this.buyPrice = buyPrice;
        this.sellPrice = sellPrice;
        this.entryTime = entryTime;
        this.exitTime = exitTime;
        this.exitReason = exitReason;
        this.grossPnL = grossPnL;
        this.netPnL = netPnL;
        this.totalCharges = totalCharges;
        this.brokerage = brokerage;
        this.stt = stt;
        this.exchangeCharges = exchangeCharges;
        this.sebiCharges = sebiCharges;
        this.stampDuty = stampDuty;
        this.gst = gst;
    }

    // Getters and Setters
    public Long getId() { return id; }
    public String getSymbol() { return symbol; }
    public Integer getQuantity() { return quantity; }
    public Double getBuyPrice() { return buyPrice; }
    public Double getSellPrice() { return sellPrice; }
    public Instant getEntryTime() { return entryTime; }
    public Instant getExitTime() { return exitTime; }
    public String getExitReason() { return exitReason; }
    public Double getGrossPnL() { return grossPnL; }
    public Double getNetPnL() { return netPnL; }
    public Double getTotalCharges() { return totalCharges; }
    public Double getBrokerage() { return brokerage; }
    public Double getStt() { return stt; }
    public Double getExchangeCharges() { return exchangeCharges; }
    public Double getSebiCharges() { return sebiCharges; }
    public Double getStampDuty() { return stampDuty; }
    public Double getGst() { return gst; }
}
