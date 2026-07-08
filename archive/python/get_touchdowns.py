import sys
import datetime
from sleeper_wrapper import League
from sleeper_wrapper import Stats

def grab_cli_args():
    cli_args = {}
    cli_args['week_id'] = sys.argv[0]

    return cli_args

def get_current_year():
    currentDateTime = datetime.datetime.now()
    date = currentDateTime.date()
    year = date.strftime("%Y")
    return year

def week_stats(week_number, year=None):
    if not year:
        year = get_current_year()

    week_stats = Stats().get_week_stats('regular', year, week_number)
    return week_stats

def main():

    args = grab_cli_args()

    # league = League(698583839592771584).get_league()
    stats_in_week = week_stats(args['week_id'])
    print(stats_in_week)

    pass

if __name__ == '__main__':
    main()
