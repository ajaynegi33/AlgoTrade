package com.algotrade.algotradebackend.scanner;

/**
 * Lightweight DTO representing a stock search and autocomplete result.
 */
public record StockSearchResult(
    String symbol,
    String companyName,
    boolean isFno,
    Long instrumentToken,
    String exchange
) {}
