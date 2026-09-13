package transparent

import "errors"

// ErrUnsupported is why transparent mode is refused anywhere but Linux. The
// mode is two Linux things at once: iptables redirects the traffic, and
// SO_ORIGINAL_DST is how the kernel says where a redirected connection was
// going. Neither has an equivalent Faultline can fall back to.
var ErrUnsupported = errors.New("transparent mode needs Linux, where iptables can redirect the traffic and the kernel remembers where it was going; on any other platform, attach with HTTP_PROXY instead")
