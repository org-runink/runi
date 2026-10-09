"""Write the dataset both pipelines read, once, so neither pays for the other's parsing."""
import csv, math, os, random

HERE = os.path.dirname(os.path.abspath(__file__))
N = 200_000
GROUPS = [f"g{i:02d}" for i in range(20)]

def main():
    rng = random.Random(20261009)
    path = os.path.join(HERE, "data", "pipeline.csv")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", newline="") as fh:
        w = csv.writer(fh)
        w.writerow(["t", "value", "driver", "group"])
        for i in range(N):
            season = 10.0 * math.sin(2 * math.pi * i / 24)
            trend = 0.002 * i
            driver = rng.gauss(0, 1)
            value = 100 + trend + season + 2.5 * driver + rng.gauss(0, 1)
            w.writerow([i, f"{value:.6f}", f"{driver:.6f}", GROUPS[i % len(GROUPS)]])
    print(f"wrote {path} ({N} rows)")

if __name__ == "__main__":
    main()
