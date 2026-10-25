package com.dcnd.frontend;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.context.annotation.Bean;
import org.springframework.web.client.RestClient;

@SpringBootApplication
public class FrontendApplication {

    public static void main(String[] args) {
        SpringApplication.run(FrontendApplication.class, args);
    }

    /**
     * RestClient used to call the backend. The javaagent instruments the
     * underlying HTTP client so {@code traceparent} is injected and the trace
     * context propagates frontend -> backend.
     */
    @Bean
    public RestClient backendRestClient() {
        String backendUrl = System.getenv().getOrDefault("BACKEND_URL", "http://localhost:3301");
        return RestClient.builder().baseUrl(backendUrl).build();
    }
}
