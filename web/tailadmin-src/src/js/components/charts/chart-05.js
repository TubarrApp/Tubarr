import ApexCharts from "apexcharts";

// ===== chartThree
const chart05 = () => {
  const chartFiveOptions = {
    series: [
      {
        name: "Sales",
        data: [180, 181, 182, 184, 183, 182, 181, 182, 183, 185, 186, 183],
      },
    ],
    legend: {
      show: false,
      position: "top",
      horizontalAlign: "left",
    },
    colors: ["#465FFF"],
    chart: {
      fontFamily: "Outfit, sans-serif",
      height: 140,
      type: "area",
      toolbar: {
        show: false,
      },
    },
    fill: {
      gradient: {
        enabled: true,
        opacityFrom: 0.55,
        opacityTo: 0,
      },
    },
    responsive: [
      {
        breakpoint: 1024,
        options: {
          chart: {
            height: 300,
          },
        },
      },
      {
        breakpoint: 1366,
        options: {
          chart: {
            height: 320,
          },
        },
      },
    ],
    stroke: {
      curve: "smooth",
      width: ["2"],
    },
    markers: {
      size: 0,
    },
    labels: {
      show: false,
      position: "top",
    },
    grid: {
      xaxis: {
        lines: {
          show: false,
        },
      },
      yaxis: {
        lines: {
          show: false,
        },
      },
    },
    dataLabels: {
      enabled: false,
    },
    tooltip: {
      x: {
        format: "dd MMM yyyy",
      },
    },
    xaxis: {
      type: "category",
      categories: [
        "Jan",
        "Feb",
        "Mar",
        "Apr",
        "May",
        "Jun",
        "Jul",
        "Aug",
        "Sep",
        "Oct",
        "Nov",
        "Dec",
      ],
      axisBorder: {
        show: false,
      },
      axisTicks: {
        show: false,
      },
      labels: {
        show: false,
      },
      tooltip: false,
    },
    yaxis: {
      labels: {
        show: false,
      },
      title: {
        style: {
          fontSize: "0px",
        },
      },
    },
  };

  const chartSelector = document.querySelectorAll("#chartFive");
  const activeUsersElement = document.querySelector(".activeUsers");

  if (chartSelector.length) {
    const chartFive = new ApexCharts(
      document.querySelector("#chartFive"),
      chartFiveOptions,
    );
    chartFive.render();

    // Simulate real-time updates
    setInterval(() => {
      // Generate new random data
      const newData = Array.from(
        { length: 12 },
        () => Math.floor(Math.random() * 50) + 150,
      );

      // Update the chart data
      chartFive.updateSeries([
        {
          name: "Sales",
          data: newData,
        },
      ]);

      // Update the active users count (random value for demo purposes)
      const newActiveUsers = Math.floor(Math.random() * 500) + 100;
      activeUsersElement.textContent = newActiveUsers;
    }, 2000);
  }
};

export default chart05;
