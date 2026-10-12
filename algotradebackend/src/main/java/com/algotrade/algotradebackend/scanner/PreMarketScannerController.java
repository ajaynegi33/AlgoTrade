package com.algotrade.algotradebackend.scanner;

import com.algotrade.algotradebackend.persistence.entity.PreMarketCandidateEntity;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.time.LocalDate;
import java.time.ZoneId;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * REST API Controller for Pre-Market Auction Scanner telemetry.
 */
@RestController
@RequestMapping("/api/scanner")
@CrossOrigin(origins = "*")
public class PreMarketScannerController {

    private final PreMarketScannerService scannerService;
    private final FnoUniverseService universeService;

    public PreMarketScannerController(
        PreMarketScannerService scannerService,
        FnoUniverseService universeService
    ) {
        this.scannerService = scannerService;
        this.universeService = universeService;
    }

    /**
     * Retrieves today's pre-market scan results, including shortlisted and rejected candidates.
     */
    @GetMapping("/latest")
    public ResponseEntity<Map<String, Object>> getLatestScanResults() {
        List<PreMarketCandidateEntity> all = scannerService.getLatestScanResults();
        List<PreMarketCandidateEntity> shortlisted = scannerService.getShortlistedCandidates();

        Map<String, Object> response = new LinkedHashMap<>();
        response.put("scanDate", LocalDate.now(ZoneId.of("Asia/Kolkata")).toString());
        response.put("totalEvaluated", all.size());
        response.put("shortlistedCount", shortlisted.size());
        response.put("lastScanTimestamp", scannerService.getLastScanTimestamp());
        response.put("shortlisted", shortlisted);
        response.put("allCandidates", all);

        return ResponseEntity.ok(response);
    }

    /**
     * Retrieves only the current shortlisted candidates.
     */
    @GetMapping("/shortlisted")
    public ResponseEntity<List<PreMarketCandidateEntity>> getShortlistedCandidates() {
        return ResponseEntity.ok(scannerService.getShortlistedCandidates());
    }

    /**
     * Manually triggers a pre-market scan run (e.g. for testing, demonstration, or off-hours verification).
     */
    @PostMapping("/trigger")
    public ResponseEntity<Map<String, Object>> triggerManualScan() {
        List<PreMarketCandidateEntity> results = scannerService.executeScan();
        List<PreMarketCandidateEntity> shortlisted = scannerService.getShortlistedCandidates();

        Map<String, Object> response = new LinkedHashMap<>();
        response.put("success", true);
        response.put("message", "Pre-market auction scan executed successfully.");
        response.put("totalEvaluated", results.size());
        response.put("shortlistedCount", shortlisted.size());
        response.put("shortlisted", shortlisted);

        return ResponseEntity.ok(response);
    }

    /**
     * Retrieves the complete dynamically discovered active F&O constituent universe.
     */
    @GetMapping("/universe")
    public ResponseEntity<Map<String, Object>> getUniverse() {
        List<String> symbols = universeService.getUniverseSymbols();
        Map<String, Object> response = new LinkedHashMap<>();
        response.put("totalFnoSymbols", symbols.size());
        response.put("totalEquities", universeService.getTotalEquitiesCount());
        response.put("lastSyncedAt", universeService.getLastSyncedAt());
        response.put("symbols", symbols);
        return ResponseEntity.ok(response);
    }

    /**
     * Manually triggers dynamic synchronization of the market universe directly from Kite instruments.
     */
    @PostMapping("/universe/sync")
    public ResponseEntity<Map<String, Object>> syncUniverse() {
        Map<String, Object> result = universeService.refreshUniverseFromExchange();
        return ResponseEntity.ok(result);
    }
}
