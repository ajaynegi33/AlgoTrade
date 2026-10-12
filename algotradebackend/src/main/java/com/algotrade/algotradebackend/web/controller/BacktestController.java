package com.algotrade.algotradebackend.web.controller;

import com.algotrade.algotradebackend.backtest.model.BacktestRequest;
import com.algotrade.algotradebackend.backtest.model.BacktestResult;
import com.algotrade.algotradebackend.backtest.service.BacktestEngineService;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.time.LocalDate;
import java.util.List;
import java.util.Map;

/**
 * REST API Controller serving the Strategy Backtest Lab.
 */
@RestController
@RequestMapping("/api/backtest")
public class BacktestController {

    private final BacktestEngineService backtestEngineService;

    public BacktestController(BacktestEngineService backtestEngineService) {
        this.backtestEngineService = backtestEngineService;
    }

    /**
     * Executes backtest simulation based on user-provided parameter configuration.
     */
    @PostMapping("/run")
    public ResponseEntity<BacktestResult> runBacktest(@RequestBody BacktestRequest request) {
        return ResponseEntity.ok(backtestEngineService.runBacktest(request));
    }

    /**
     * Returns curated historical preset periods for quick 1-click backtesting.
     */
    @GetMapping("/presets")
    public ResponseEntity<List<Map<String, String>>> getPresets() {
        LocalDate today = LocalDate.now();
        return ResponseEntity.ok(List.of(
            Map.of(
                "id", "30D",
                "label", "Last 30 Days",
                "startDate", today.minusDays(30).toString(),
                "endDate", today.toString(),
                "description", "Recent market momentum & regime check"
            ),
            Map.of(
                "id", "90D",
                "label", "Last 90 Days (Quarterly)",
                "startDate", today.minusDays(90).toString(),
                "endDate", today.toString(),
                "description", "Standard 1-quarter statistical sample"
            ),
            Map.of(
                "id", "180D",
                "label", "Last 6 Months",
                "startDate", today.minusDays(180).toString(),
                "endDate", today.toString(),
                "description", "Multi-month bull/bear cycle stress test"
            ),
            Map.of(
                "id", "YTD",
                "label", "Year to Date (YTD)",
                "startDate", LocalDate.of(today.getYear(), 1, 1).toString(),
                "endDate", today.toString(),
                "description", "Full calendar year performance"
            )
        ));
    }
}
