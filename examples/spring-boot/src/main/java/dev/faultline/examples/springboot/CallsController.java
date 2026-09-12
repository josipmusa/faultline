package dev.faultline.examples.springboot;

import java.time.Duration;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.ResponseEntity;
import org.springframework.http.client.reactive.ReactorClientHttpConnector;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.client.RestClient;
import org.springframework.web.client.RestClientException;
import org.springframework.web.client.RestClientResponseException;
import org.springframework.web.reactive.function.client.WebClient;

import reactor.netty.http.client.HttpClient;

/**
 * Three endpoints that make the same outbound call. Two of them use the two
 * HTTP clients a Spring application is likely to use, and exist so Faultline
 * can be pointed at both: {@code RestClient} runs on the JDK's own HTTP stack and picks up
 * the {@code http.proxyHost} system properties, while {@code WebClient} runs
 * on Reactor Netty, which has to be told to read them with
 * {@code proxyWithSystemProperties()}.
 *
 * <p>Failures are deliberately not caught by those two. A fault injected into
 * the upstream should surface as a failed request here, the way the real thing
 * would. {@code /rest-client-retrying} is the counterpart that recovers: same
 * call, wrapped in the backoff a service that cared about this dependency
 * would have written.
 */
@RestController
class CallsController {

	private static final Logger log = LoggerFactory.getLogger(CallsController.class);

	/** How many attempts {@code /rest-client-retrying} makes before it gives up. */
	private static final int MAX_ATTEMPTS = 4;

	/** How long it waits after the first failure. Every further wait doubles. */
	private static final Duration FIRST_WAIT = Duration.ofMillis(200);

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

	/**
	 * The same call as {@code /rest-client}, retried with exponential backoff
	 * and given up on after {@link #MAX_ATTEMPTS} attempts. The loop is written
	 * out rather than reached for from a library because what it costs the
	 * caller - the attempts, and the time spent waiting between them - is the
	 * thing worth reading here, and it is what Faultline's report counts.
	 *
	 * <p>A 4xx is not repeated: the upstream understood the request and
	 * rejected it, so trying again would only reach the same answer.
	 */
	@GetMapping("/rest-client-retrying")
	RetriedCall restClientRetrying() throws InterruptedException {
		long started = System.nanoTime();
		long waitedMs = 0;
		Duration wait = FIRST_WAIT;

		for (int attempt = 1;; attempt++) {
			try {
				ResponseEntity<String> response = restClient.get().uri(path).retrieve().toEntity(String.class);
				log.info("attempt {} of {} answered {} after {}ms of waiting", attempt, MAX_ATTEMPTS,
						response.getStatusCode().value(), waitedMs);
				return RetriedCall.of(Call.of("RestClient", response, started), attempt, waitedMs);
			}
			catch (RestClientException e) {
				if (attempt == MAX_ATTEMPTS || notWorthRepeating(e)) {
					log.warn("attempt {} of {} failed and was the last: {}", attempt, MAX_ATTEMPTS, e.getMessage());
					throw e;
				}
				log.warn("attempt {} of {} failed ({}), retrying in {}ms", attempt, MAX_ATTEMPTS, e.getMessage(),
						wait.toMillis());
				Thread.sleep(wait);
				waitedMs += wait.toMillis();
				wait = wait.multipliedBy(2);
			}
		}
	}

	private static boolean notWorthRepeating(RestClientException e) {
		return e instanceof RestClientResponseException response && response.getStatusCode().is4xxClientError();
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

	/**
	 * What one retried call turned into: the attempt that ended it, plus what
	 * reaching that attempt cost. {@code durationMs} covers every attempt and
	 * the waits between them, so it is what the caller actually sat through.
	 */
	record RetriedCall(String client, int status, long durationMs, int bytes, int attempts, long waitedMs) {

		static RetriedCall of(Call call, int attempts, long waitedMs) {
			return new RetriedCall(call.client(), call.status(), call.durationMs(), call.bytes(), attempts, waitedMs);
		}
	}
}
