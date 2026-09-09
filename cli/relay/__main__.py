import argparse
from importlib.metadata import version

def get_version():
    try:
        return version("relay")
    except:
        return "No current version (note to self it didn't work)"




def main():
    ver=get_version()
    parser = argparse.ArgumentParser(prog="relay")
    parser.add_argument("-v","--version",action="version",help="show relay version")
    parser.parse_args()


if __name__ == "__main__":
    main()
