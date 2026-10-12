package com.algotrade.algotradebackend.config;

import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.config.annotation.web.configuration.EnableWebSecurity;
import org.springframework.security.config.annotation.web.configurers.AbstractHttpConfigurer;
import org.springframework.security.web.SecurityFilterChain;
import org.springframework.web.cors.CorsConfiguration;
import org.springframework.web.cors.CorsConfigurationSource;
import org.springframework.web.cors.UrlBasedCorsConfigurationSource;

import java.util.List;

/**
 * Security Configuration for the AlgoTrade backend.
 * 
 * <p>Configures Cross-Origin Resource Sharing (CORS) to allow our Next.js frontend
 * (whether running on localhost:3000 during development or on Vercel in production)
 * to communicate seamlessly with our Spring Boot REST and WebSocket endpoints.</p>
 * 
 * <p>Disables CSRF for stateless REST APIs and permits public access to trading control,
 * dashboard metrics, and health-check endpoints.</p>
 */
@Configuration
@EnableWebSecurity
public class SecurityConfig {

    @Bean
    public SecurityFilterChain securityFilterChain(HttpSecurity http) throws Exception {
        http
            // Disable CSRF since this is a stateless trading API consumed by Next.js
            .csrf(AbstractHttpConfigurer::disable)
            // Enable CORS with our custom configuration source defined below
            .cors(cors -> cors.configurationSource(corsConfigurationSource()))
            // Authorize requests: All trading endpoints and WebSockets are permitted
            .authorizeHttpRequests(auth -> auth
                .requestMatchers("/api/**", "/ws/**", "/actuator/**").permitAll()
                .anyRequest().permitAll()
            );

        return http.build();
    }

    /**
     * Defines permissive CORS rules for our Next.js frontend.
     * Allows requests from localhost:3000 and any Vercel deployment preview domain.
     */
    @Bean
    public CorsConfigurationSource corsConfigurationSource() {
        CorsConfiguration config = new CorsConfiguration();
        // Allow local dev Next.js and Vercel domains
        config.setAllowedOriginPatterns(List.of("http://localhost:3000", "https://*.vercel.app", "*"));
        config.setAllowedMethods(List.of("GET", "POST", "PUT", "DELETE", "OPTIONS"));
        config.setAllowedHeaders(List.of("*"));
        config.setAllowCredentials(true);

        UrlBasedCorsConfigurationSource source = new UrlBasedCorsConfigurationSource();
        source.registerCorsConfiguration("/**", config);
        return source;
    }
}
