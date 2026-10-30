/*
 * gh-ndpi: names the application behind each network flow using the nDPI library.
 *
 * It listens on the LAN ports it is told about, runs nDPI on the first packets of every flow, and
 * sends one short JSON line per flow to Gatehouse over a local socket. It never stores or sends
 * packet contents, only: addresses, ports, the application name, its category and any warnings.
 *
 *   gh-ndpi -s /run/gatehouse/ndpi.sock eth1 eth2     (live)
 *   gh-ndpi -r capture.pcap                           (read a capture, print the lines; for testing)
 *
 * It is a separate program, run by systemd with almost no rights, so a fault in the inspection
 * library can never reach the router's settings. If it is not installed, Gatehouse carries on.
 */
#include <arpa/inet.h>
#include <errno.h>
#include <netinet/in.h>
#include <pcap.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <time.h>
#include <unistd.h>

#include <ndpi/ndpi_api.h>

#define MAX_FLOWS 50000
#define IDLE_SECS 60
#define GIVE_UP_AFTER 24 /* packets */
#define MAX_IFACES 8

struct key {
    uint32_t a, b;
    uint16_t pa, pb;
    uint8_t proto;
};

struct flow {
    struct key k;
    uint32_t src, dst; /* as first seen */
    uint16_t sport, dport;
    uint8_t proto;
    uint8_t done;
    unsigned packets;
    time_t last;
    struct ndpi_flow_struct *f;
    struct flow *next;
};

static struct ndpi_detection_module_struct *mod;
static struct flow *table[1 << 16];
static unsigned nflows;
static int sock = -1;
static const char *sockpath;
static int offline;
static time_t last_connect;

static unsigned hash(const struct key *k) {
    uint32_t h = k->a * 2654435761u ^ k->b * 40503u ^ ((uint32_t)k->pa << 16 | k->pb) ^ k->proto;
    return (h ^ (h >> 16)) & 0xffff;
}

static void free_flow(struct flow *x) {
    if (x->f) ndpi_free_flow(x->f);
    free(x);
}

static void sweep(time_t now, int all) {
    for (unsigned i = 0; i < (1 << 16); i++) {
        struct flow **pp = &table[i];
        while (*pp) {
            struct flow *x = *pp;
            if (all || now - x->last > IDLE_SECS) {
                *pp = x->next;
                free_flow(x);
                nflows--;
            } else
                pp = &x->next;
        }
    }
}

static void json_str(char *out, size_t n, const char *s) {
    size_t j = 0;
    for (; *s && j + 7 < n; s++) {
        unsigned char c = (unsigned char)*s;
        if (c == '"' || c == '\\') { out[j++] = '\\'; out[j++] = c; }
        else if (c < 0x20 || c >= 0x7f) out[j++] = '_';
        else out[j++] = c;
    }
    out[j] = 0;
}

static void emit(const char *line) {
    if (offline) { fputs(line, stdout); return; }
    time_t now = time(NULL);
    if (sock < 0 && now - last_connect >= 5) {
        last_connect = now;
        int s = socket(AF_UNIX, SOCK_STREAM, 0);
        struct sockaddr_un sa = {.sun_family = AF_UNIX};
        strncpy(sa.sun_path, sockpath, sizeof(sa.sun_path) - 1);
        if (s >= 0 && connect(s, (struct sockaddr *)&sa, sizeof sa) == 0) sock = s;
        else if (s >= 0) close(s);
    }
    if (sock < 0) return; /* nobody is listening: drop, never block */
    if (send(sock, line, strlen(line), MSG_DONTWAIT | MSG_NOSIGNAL) < 0 && errno != EAGAIN) {
        close(sock);
        sock = -1;
    }
}

static void report(struct flow *x, ndpi_protocol p) {
    char app[64], cat[64], line[640], sa[16], da[16], an[96], cn[96];
    ndpi_protocol2name(mod, p, app, sizeof app);
    snprintf(cat, sizeof cat, "%s", ndpi_category_get_name(mod, p.category));
    inet_ntop(AF_INET, &x->src, sa, sizeof sa);
    inet_ntop(AF_INET, &x->dst, da, sizeof da);
    json_str(an, sizeof an, app);
    json_str(cn, sizeof cn, cat);
    /* warnings: the first few risks nDPI raised for this flow */
    char risks[256] = "";
    size_t rl = 0;
    int nr = 0;
    for (int r = 1; r < NDPI_MAX_RISK && nr < 3; r++) {
        if (NDPI_ISSET_BIT(x->f->risk, r)) {
            char nm[96];
            json_str(nm, sizeof nm, ndpi_risk2str((ndpi_risk_enum)r));
            rl += snprintf(risks + rl, sizeof risks - rl, "%s\"%s\"", nr ? "," : "", nm);
            nr++;
        }
    }
    snprintf(line, sizeof line, "{\"proto\":\"%s\",\"src\":\"%s\",\"sp\":%u,\"dst\":\"%s\",\"dp\":%u,\"app\":\"%s\",\"cat\":\"%s\",\"risk\":[%s]}\n",
             x->proto == IPPROTO_TCP ? "tcp" : "udp", sa, x->sport, da, x->dport, an, cn, risks);
    emit(line);
}

static void packet(const u_char *ip, unsigned len, time_t sec, uint64_t ms) {
    if (len < 20 || (ip[0] >> 4) != 4) return; /* IPv4 only for now */
    unsigned ihl = (ip[0] & 15) * 4;
    uint8_t proto = ip[9];
    if ((proto != IPPROTO_TCP && proto != IPPROTO_UDP) || len < ihl + 8) return;
    uint32_t s, d;
    memcpy(&s, ip + 12, 4);
    memcpy(&d, ip + 16, 4);
    uint16_t sp = ntohs(*(uint16_t *)(ip + ihl)), dp = ntohs(*(uint16_t *)(ip + ihl + 2));
    struct key k;
    int fwd = s < d || (s == d && sp <= dp);
    k.a = fwd ? s : d; k.b = fwd ? d : s;
    k.pa = fwd ? sp : dp; k.pb = fwd ? dp : sp;
    k.proto = proto;
    unsigned h = hash(&k);
    struct flow *x = table[h];
    while (x && memcmp(&x->k, &k, sizeof k)) x = x->next;
    if (!x) {
        if (nflows >= MAX_FLOWS) sweep(sec, 0);
        if (nflows >= MAX_FLOWS) return; /* full of live flows: skip rather than grow */
        x = calloc(1, sizeof *x);
        if (!x) return;
        x->f = ndpi_flow_malloc(SIZEOF_FLOW_STRUCT);
        if (!x->f) { free(x); return; }
        memset(x->f, 0, SIZEOF_FLOW_STRUCT);
        x->k = k; x->src = s; x->dst = d; x->sport = sp; x->dport = dp; x->proto = proto;
        x->next = table[h];
        table[h] = x;
        nflows++;
    }
    x->last = sec;
    if (x->done) return;
    x->packets++;
#ifdef GH_NDPI_NEWAPI
    ndpi_protocol p = ndpi_detection_process_packet(mod, x->f, ip, len, ms, NULL);
#else
    ndpi_protocol p = ndpi_detection_process_packet(mod, x->f, ip, len, ms);
#endif
    int known = p.app_protocol != NDPI_PROTOCOL_UNKNOWN || p.master_protocol != NDPI_PROTOCOL_UNKNOWN;
    int certain = known && p.app_protocol != NDPI_PROTOCOL_TLS && p.app_protocol != NDPI_PROTOCOL_UNKNOWN;
    if (certain || x->packets >= GIVE_UP_AFTER) {
        if (!certain) {
#ifdef GH_NDPI_NEWAPI
            p = ndpi_detection_giveup(mod, x->f);
#else
            u_int8_t guessed = 0;
            p = ndpi_detection_giveup(mod, x->f, 1, &guessed);
#endif
        }
        x->done = 1;
        if (p.app_protocol != NDPI_PROTOCOL_UNKNOWN || p.master_protocol != NDPI_PROTOCOL_UNKNOWN) report(x, p);
    }
}

static void handle(u_char *u, const struct pcap_pkthdr *h, const u_char *bytes) {
    int dlt = *(int *)u;
    unsigned off = 0;
    if (dlt == DLT_EN10MB) {
        if (h->caplen < 14) return;
        uint16_t t = ntohs(*(uint16_t *)(bytes + 12));
        off = 14;
        while ((t == 0x8100 || t == 0x88a8) && h->caplen >= off + 4) { t = ntohs(*(uint16_t *)(bytes + off + 2)); off += 4; }
        if (t != 0x0800) return;
    } else if (dlt == DLT_LINUX_SLL) {
        if (h->caplen < 16 || ntohs(*(uint16_t *)(bytes + 14)) != 0x0800) return;
        off = 16;
    } else
        return;
    packet(bytes + off, h->caplen - off, h->ts.tv_sec, (uint64_t)h->ts.tv_sec * 1000 + h->ts.tv_usec / 1000);
}

int main(int argc, char **argv) {
    const char *file = NULL;
    int i = 1;
    for (; i < argc && argv[i][0] == '-'; i++) {
        if (!strcmp(argv[i], "-s") && i + 1 < argc) sockpath = argv[++i];
        else if (!strcmp(argv[i], "-r") && i + 1 < argc) { file = argv[++i]; offline = 1; }
        else { fprintf(stderr, "usage: gh-ndpi -s SOCKET IFACE... | -r FILE\n"); return 2; }
    }
    if (!offline && (!sockpath || i >= argc)) { fprintf(stderr, "usage: gh-ndpi -s SOCKET IFACE... | -r FILE\n"); return 2; }
#ifdef GH_NDPI_NEWAPI
    mod = ndpi_init_detection_module(NULL);
#else
    mod = ndpi_init_detection_module(ndpi_no_prefs);
#endif
    if (!mod) { fprintf(stderr, "gh-ndpi: the library did not start\n"); return 1; }
    NDPI_PROTOCOL_BITMASK all;
    NDPI_BITMASK_SET_ALL(all);
    ndpi_set_protocol_detection_bitmask2(mod, &all);
    ndpi_finalize_initialization(mod);

    char err[PCAP_ERRBUF_SIZE];
    if (offline) {
        pcap_t *pc = pcap_open_offline(file, err);
        if (!pc) { fprintf(stderr, "gh-ndpi: %s\n", err); return 1; }
        int dlt = pcap_datalink(pc);
        pcap_loop(pc, -1, handle, (u_char *)&dlt);
        pcap_close(pc);
        sweep(0, 1);
        ndpi_exit_detection_module(mod);
        return 0;
    }
    pcap_t *pcs[MAX_IFACES];
    int dlts[MAX_IFACES], n = 0;
    struct pollfd pfds[MAX_IFACES];
    for (; i < argc && n < MAX_IFACES; i++) {
        pcap_t *pc = pcap_open_live(argv[i], 1600, 0, 100, err);
        if (!pc) { fprintf(stderr, "gh-ndpi: %s: %s\n", argv[i], err); continue; }
        pcap_setnonblock(pc, 1, err);
        pcs[n] = pc; dlts[n] = pcap_datalink(pc);
        pfds[n].fd = pcap_get_selectable_fd(pc); pfds[n].events = POLLIN;
        n++;
    }
    if (!n) return 1;
    time_t lastsweep = time(NULL);
    for (;;) {
        poll(pfds, n, 1000);
        for (int j = 0; j < n; j++) pcap_dispatch(pcs[j], 256, handle, (u_char *)&dlts[j]);
        time_t now = time(NULL);
        if (now - lastsweep >= 10) { sweep(now, 0); lastsweep = now; }
    }
}
