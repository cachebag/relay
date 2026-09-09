import argparse
from importlib.metadata import version


def main():
    parser = argparse.ArgumentParser(prog="relay")
    parser.add_argument(
        "-v",
        "--version",
        action="version",
        version=f"relay {version('relay')}",
        help="show relay version",
    )
    parser.parse_args()


if __name__ == "__main__":
    main()
