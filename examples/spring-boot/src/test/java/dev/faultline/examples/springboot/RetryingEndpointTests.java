package dev.faultline.examples.springboot;

import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.atomic.AtomicInteger;

import com.sun.net.httpserver.HttpServer;

import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.springframework.web.client.RestClient;
import org.springframework.web.client.RestClientResponseException;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatExceptionOfType;

/**
 * The retrying endpoint against an upstream that fails the first two calls and
 * then recovers, which is what a {@code first_n} 503 rule does to it. No
 * Faultline here: the stub reproduces the fault so the example's own backoff
 * is what is under test, and the report numbers it produces are verified
 * against a real instance by the task's manual step.
 */
@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
class RetryingEndpointTests {

	/** How many of the upstream's calls fail before it starts answering. */
	private static final int FAILURES = 2;

	/** How many attempts the endpoint makes before it gives up. */
	private static final int MAX_ATTEMPTS = 4;

	private static HttpServer upstream;

	private static final AtomicInteger calls = new AtomicInteger();

	@LocalServerPort
	private int port;

	@BeforeAll
	static void startUpstream() throws IOException {
		upstream = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
		upstream.createContext("/get", exchange -> {
			boolean fail = calls.incrementAndGet() <= FAILURES;
			byte[] body = (fail ? "upstream is down" : "upstream is up").getBytes(StandardCharsets.UTF_8);
			exchange.sendResponseHeaders(fail ? 503 : 200, body.length);
			try (OutputStream out = exchange.getResponseBody()) {
				out.write(body);
			}
		});
		upstream.start();
	}

	@AfterAll
	static void stopUpstream() {
		upstream.stop(0);
	}

	@DynamicPropertySource
	static void upstreamAddress(DynamicPropertyRegistry registry) {
		registry.add("example.upstream", () -> "http://127.0.0.1:" + upstream.getAddress().getPort());
	}

	@BeforeEach
	void resetUpstream() {
		calls.set(0);
	}

	@Test
	void retriesPastTheFailuresAndSucceedsOnTheRecovery() {
		CallsController.RetriedCall call = app().get().uri("/rest-client-retrying").retrieve()
				.body(CallsController.RetriedCall.class);

		assertThat(call).isNotNull();
		assertThat(call.attempts()).as("two failures then the recovery").isEqualTo(FAILURES + 1);
		assertThat(calls.get()).as("every attempt reached the upstream").isEqualTo(FAILURES + 1);
		assertThat(call.status()).isEqualTo(200);
		assertThat(call.waitedMs()).as("it backed off between the attempts").isPositive();
	}

	@Test
	void givesUpWhenTheUpstreamNeverRecovers() {
		calls.set(Integer.MIN_VALUE); // every call fails: the counter never reaches FAILURES.

		assertThatExceptionOfType(RestClientResponseException.class)
				.isThrownBy(() -> app().get().uri("/rest-client-retrying").retrieve().toBodilessEntity());

		assertThat(calls.get() - Integer.MIN_VALUE).as("it stopped trying").isEqualTo(MAX_ATTEMPTS);
	}

	@Test
	void theNonRetryingEndpointMakesOneAttempt() {
		assertThatExceptionOfType(RestClientResponseException.class)
				.isThrownBy(() -> app().get().uri("/rest-client").retrieve().toBodilessEntity());

		assertThat(calls.get()).as("the fault surfaced uncaught, unrepeated").isEqualTo(1);
	}

	private RestClient app() {
		return RestClient.create("http://127.0.0.1:" + port);
	}
}
