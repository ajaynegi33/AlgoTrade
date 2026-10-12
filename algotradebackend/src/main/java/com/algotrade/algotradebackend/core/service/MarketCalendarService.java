package com.algotrade.algotradebackend.core.service;

import org.springframework.stereotype.Service;

import java.time.DayOfWeek;
import java.time.LocalTime;
import java.time.ZoneId;
import java.time.ZonedDateTime;

/**
 * Service managing the official National Stock Exchange (NSE) trading calendar and session timings.
 * 
 * <p>Official NSE Equities market timings:
 * <ul>
 *   <li>Pre-open matching: 09:00 AM - 09:15 AM IST</li>
 *   <li>Regular market open: 09:15:00 AM IST</li>
 *   <li>15-Minute Observation Window: 09:15:00 AM - 09:30:00 AM IST</li>
 *   <li>Square-Off cutoff: 03:15:00 PM IST</li>
 *   <li>Normal market close: 03:30:00 PM IST</li>
 *   <li>Trading Days: Monday through Friday (excluding weekends and public holidays)</li>
 * </ul>
 * </p>
 */
@Service
public class MarketCalendarService {

    public static final ZoneId IST_ZONE = ZoneId.of("Asia/Kolkata");
    public static final LocalTime MARKET_OPEN_TIME = LocalTime.of(9, 15);
    public static final LocalTime OBSERVATION_END_TIME = LocalTime.of(9, 30);
    public static final LocalTime SQUARE_OFF_TIME = LocalTime.of(15, 15);
    public static final LocalTime MARKET_CLOSE_TIME = LocalTime.of(15, 30);

    /**
     * Checks whether the NSE equities market is currently open for regular live trading.
     * 
     * @return true strictly between 09:15 AM and 03:30 PM IST on Monday through Friday.
     */
    public boolean isMarketOpen() {
        ZonedDateTime now = ZonedDateTime.now(IST_ZONE);
        DayOfWeek day = now.getDayOfWeek();
        if (day == DayOfWeek.SATURDAY || day == DayOfWeek.SUNDAY) {
            return false;
        }

        LocalTime time = now.toLocalTime();
        return !time.isBefore(MARKET_OPEN_TIME) && time.isBefore(MARKET_CLOSE_TIME);
    }

    /**
     * Checks if the current moment is inside the initial 15-minute observation window (09:15 AM - 09:30 AM).
     */
    public boolean isObservationWindow() {
        if (!isMarketOpen()) {
            return false;
        }
        LocalTime time = ZonedDateTime.now(IST_ZONE).toLocalTime();
        return !time.isBefore(MARKET_OPEN_TIME) && time.isBefore(OBSERVATION_END_TIME);
    }

    /**
     * Human-readable description of when the next market session will open.
     */
    public String getNextMarketOpenFormatted() {
        ZonedDateTime now = ZonedDateTime.now(IST_ZONE);
        DayOfWeek day = now.getDayOfWeek();
        LocalTime time = now.toLocalTime();

        if (isMarketOpen()) {
            return "Market Open Now (Closes at 03:30 PM IST)";
        }

        if (day == DayOfWeek.SATURDAY) {
            return "Monday at 09:15 AM IST";
        }
        if (day == DayOfWeek.SUNDAY) {
            return "Tomorrow (Monday) at 09:15 AM IST";
        }

        // Monday to Friday
        if (time.isBefore(MARKET_OPEN_TIME)) {
            return "Today at 09:15 AM IST";
        }

        // After market hours on a weekday
        if (day == DayOfWeek.FRIDAY) {
            return "Monday at 09:15 AM IST";
        } else {
            return "Tomorrow at 09:15 AM IST";
        }
    }
}
