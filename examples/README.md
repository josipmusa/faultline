# Examples

Four small applications, one per runtime Faultline has to work with. They are
what the roadmap's verification steps run against, and each is deliberately
ordinary: none of them knows Faultline exists.

| Example | Runtime | Start command |
| --- | --- | --- |
| [go-client](go-client) | Go, stdlib | `go run ./examples/go-client` |
| [node-fetch](node-fetch) | Node 22, native `fetch` | `node examples/node-fetch/index.js` |
| [spring-boot](spring-boot) | Java 21, Spring Boot | `./mvnw spring-boot:run` |
| [vite-frontend](vite-frontend) | React, Vite dev server | `npm run dev` |

All four call `https://httpbin.org` by default, and each README says how to
point it elsewhere and how to run it through Faultline.

[compose](compose) is not a fifth application. It is the Spring Boot one again,
in its own container with Faultline as a companion next to it, which is how a
service is attached when it runs the way production runs it.

[compose-transparent](compose-transparent) is the Go one in the same shape, with
the other attach mode: it shares Faultline's network namespace and is redirected
into it by iptables, so it has no proxy variables and no change to its image at
all.

`vite-frontend` is the odd one out: the browser is not a child of
`faultline run`, so its traffic is caught by pointing the Vite dev proxy at a
Faultline route instead. Its README explains the arrangement.
