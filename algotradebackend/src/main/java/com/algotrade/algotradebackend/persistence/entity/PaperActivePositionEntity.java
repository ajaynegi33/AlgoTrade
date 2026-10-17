package com.algotrade.algotradebackend.persistence.entity;

import jakarta.persistence.*;
import java.time.Instant;

/**
 * JPA Entity representing an active, currently held open paper trade position.
 * Persisted in `algotradepaper.db` so active paper positions survive backend restarts.
 */
@Entity
@Table(name = "paper_active_positions")
public class PaperActivePositionEntity {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(nullable = false)
    private String symbol;

    @Column(nullable = false)
    private Integer quantity;

    @Column(nullable = false)
    private Double entryPrice;

    @Column(nullable = false)
    private Double currentPrice;

    @Column(nullable = false)
    private Double initialStopLoss;

    @Column(nullable = false)
    private Double activeTrailingStop;

    @Column(nullable = false)
    private Double highWaterMark;

    @Column(nullable = false)
    private Instant entryTime;

    @Column
    private String activeSlOrderId;

    @Column
    private String mode; // PAPER or LIVE

    @Column
    private Double initialStopLimit;

    @Column
    private Double unitRisk;

    @Column
    private Integer logicalStage;

    @Column
    private Double armedLimit;

    @Column
    private Integer slModCount;

    @Column
    private String pendingOp;

    @Column
    private Double pendingTrigger;

    @Column
    private Double pendingLimit;

    @Column
    private String exitPolicy;

    @Column
    private String exitOrderId;

    @Column
    private Instant hwmTime;

    @Column
    private Instant lastHwmStepTime;

    @Column
    private Integer latchesMask;

    @Column
    private Instant updatedAt;

    public PaperActivePositionEntity() {}

    public PaperActivePositionEntity(
        String symbol,
        Integer quantity,
        Double entryPrice,
        Double currentPrice,
        Double initialStopLoss,
        Double activeTrailingStop,
        Double highWaterMark,
        Instant entryTime,
        String activeSlOrderId
    ) {
        this.symbol = symbol;
        this.quantity = quantity;
        this.entryPrice = entryPrice;
        this.currentPrice = currentPrice;
        this.initialStopLoss = initialStopLoss;
        this.activeTrailingStop = activeTrailingStop;
        this.highWaterMark = highWaterMark;
        this.entryTime = entryTime;
        this.activeSlOrderId = activeSlOrderId;
        this.logicalStage = 0;
        this.slModCount = 0;
        this.latchesMask = 0;
        this.updatedAt = Instant.now();
    }

    public Long getId() { return id; }
    public String getSymbol() { return symbol; }
    public void setSymbol(String symbol) { this.symbol = symbol; }

    public Integer getQuantity() { return quantity; }
    public void setQuantity(Integer quantity) { this.quantity = quantity; }

    public Double getEntryPrice() { return entryPrice; }
    public void setEntryPrice(Double entryPrice) { this.entryPrice = entryPrice; }

    public Double getCurrentPrice() { return currentPrice; }
    public void setCurrentPrice(Double currentPrice) { this.currentPrice = currentPrice; }

    public Double getInitialStopLoss() { return initialStopLoss; }
    public void setInitialStopLoss(Double initialStopLoss) { this.initialStopLoss = initialStopLoss; }

    public Double getActiveTrailingStop() { return activeTrailingStop; }
    public void setActiveTrailingStop(Double activeTrailingStop) { this.activeTrailingStop = activeTrailingStop; }

    public Double getHighWaterMark() { return highWaterMark; }
    public void setHighWaterMark(Double highWaterMark) { this.highWaterMark = highWaterMark; }

    public Instant getEntryTime() { return entryTime; }
    public void setEntryTime(Instant entryTime) { this.entryTime = entryTime; }

    public String getActiveSlOrderId() { return activeSlOrderId; }
    public void setActiveSlOrderId(String activeSlOrderId) { this.activeSlOrderId = activeSlOrderId; }

    public String getMode() { return mode; }
    public void setMode(String mode) { this.mode = mode; }

    public Double getInitialStopLimit() { return initialStopLimit; }
    public void setInitialStopLimit(Double initialStopLimit) { this.initialStopLimit = initialStopLimit; }

    public Double getUnitRisk() { return unitRisk; }
    public void setUnitRisk(Double unitRisk) { this.unitRisk = unitRisk; }

    public Integer getLogicalStage() { return logicalStage != null ? logicalStage : 0; }
    public void setLogicalStage(Integer logicalStage) { this.logicalStage = logicalStage; }

    public Double getArmedLimit() { return armedLimit; }
    public void setArmedLimit(Double armedLimit) { this.armedLimit = armedLimit; }

    public Integer getSlModCount() { return slModCount != null ? slModCount : 0; }
    public void setSlModCount(Integer slModCount) { this.slModCount = slModCount; }

    public String getPendingOp() { return pendingOp; }
    public void setPendingOp(String pendingOp) { this.pendingOp = pendingOp; }

    public Double getPendingTrigger() { return pendingTrigger; }
    public void setPendingTrigger(Double pendingTrigger) { this.pendingTrigger = pendingTrigger; }

    public Double getPendingLimit() { return pendingLimit; }
    public void setPendingLimit(Double pendingLimit) { this.pendingLimit = pendingLimit; }

    public String getExitPolicy() { return exitPolicy; }
    public void setExitPolicy(String exitPolicy) { this.exitPolicy = exitPolicy; }

    public String getExitOrderId() { return exitOrderId; }
    public void setExitOrderId(String exitOrderId) { this.exitOrderId = exitOrderId; }

    public Instant getHwmTime() { return hwmTime; }
    public void setHwmTime(Instant hwmTime) { this.hwmTime = hwmTime; }

    public Instant getLastHwmStepTime() { return lastHwmStepTime; }
    public void setLastHwmStepTime(Instant lastHwmStepTime) { this.lastHwmStepTime = lastHwmStepTime; }

    public Integer getLatchesMask() { return latchesMask != null ? latchesMask : 0; }
    public void setLatchesMask(Integer latchesMask) { this.latchesMask = latchesMask; }

    public Instant getUpdatedAt() { return updatedAt; }
    public void setUpdatedAt(Instant updatedAt) { this.updatedAt = updatedAt; }

    // Latches bitmask helpers
    public boolean isLatchSet(int bit) {
        return (getLatchesMask() & bit) != 0;
    }

    public void setLatch(int bit) {
        this.latchesMask = getLatchesMask() | bit;
    }
}
