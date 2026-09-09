package dev.faultline.examples.springboot;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.ResponseEntity;
import org.springframework.http.client.reactive.ReactorClientHttpConnector;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.client.RestClient;
import org.springframework.web.reactive.function.client.WebClient;

import reactor.netty.http.client.HttpClient;

/**
 * Two endpoints that make the same outbound call with the two HTTP clients a
 * Spring application is likely to use. They exist so Faultline can be pointed
 * at both: {@code RestClient} runs on the JDK's own HTTP stack and picks up
 * the {@code http.proxyHost} system properties, while {@code WebClient} runs
 * on Reactor Netty, which has to be told to read them with
 * {@code proxyWithSystemProperties()}.
 *
 * <p>Failures are deliberately not caught. A fault injected into the upstream
 * should surface as a failed request here, the way the real thing would.
 */
@RestController
class CallsController {

	private final RestClient restClient;
	private final WebClient webClient;
	private final String path;

	CallsController(RestClient.Builder restClients, WebClient.Builder webClients,
			@Value("${example.upstream}") String upstream, @Value("${example.path}") String path) {
		this.restClient = restClients.baseUrl(upstream).build();
		// Reactor Netty ignores the http.proxyHost system properties unless it
		// is asked to read them, so a WebClient built the default way goes
		// straight to the upstream while everything else in the same JVM goes
		// through the proxy. Trust is not affected: the JDK trust store the
		// javax.net.ssl properties name is used either way.
		this.webClient = webClients.baseUrl(upstream)
				.clientConnector(new ReactorClientHttpConnector(HttpClient.create().proxyWithSystemProperties()))
				.build();
		this.path = path;
	}

	@GetMapping("/rest-client")
	Call restClient() {
		long started = System.nanoTime();
		ResponseEntity<String> response = restClient.get().uri(path).retrieve().toEntity(String.class);
		return Call.of("RestClient", response, started);
	}

	@GetMapping("/web-client")
	Call webClient() {
		long started = System.nanoTime();
		ResponseEntity<String> response = webClient.get().uri(path).retrieve().toEntity(String.class).block();
		return Call.of("WebClient", response, started);
	}

	/** What one outbound call turned into, as this app saw it. */
	record Call(String client, int status, long durationMs, int bytes) {

		static Call of(String client, ResponseEntity<String> response, long startedNanos) {
			long durationMs = (System.nanoTime() - startedNanos) / 1_000_000;
			String body = response == null ? null : response.getBody();
			int status = response == null ? 0 : response.getStatusCode().value();
			return new Call(client, status, durationMs, body == null ? 0 : body.length());
		}
	}
}
